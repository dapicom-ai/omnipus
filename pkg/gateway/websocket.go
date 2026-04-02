// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/dapicom-ai/omnipus/pkg/agent"
	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/session"
)

// wsClientFrame is a message sent from the browser to the server over WebSocket.
type wsClientFrame struct {
	Type      string `json:"type"`                  // "auth" | "message" | "cancel" | "exec_approval_response" | "attach_session"
	Token     string `json:"token,omitempty"`       // for "auth"
	Content   string `json:"content,omitempty"`     // for "message"
	SessionID string `json:"session_id,omitempty"`  // for "message" / "cancel" / "attach_session"
	AgentID   string `json:"agent_id,omitempty"`    // for "message" — route to specific agent
	ID        string `json:"id,omitempty"`           // for "exec_approval_response"
	Decision  string `json:"decision,omitempty"`    // "allow" | "deny" | "always"
}

// wsServerFrame is a message sent from the server to the browser over WebSocket.
type wsServerFrame struct {
	Type       string         `json:"type"`
	Content    string         `json:"content,omitempty"`
	Role       string         `json:"role,omitempty"`
	Tool       string         `json:"tool,omitempty"`
	Params     map[string]any `json:"params,omitempty"`
	Result     any            `json:"result,omitempty"`
	Command    string         `json:"command,omitempty"`
	ID         string         `json:"id,omitempty"`
	CallID     string         `json:"call_id,omitempty"`
	Stats      map[string]any `json:"stats,omitempty"`
	Message    string         `json:"message,omitempty"`
	Status     string         `json:"status,omitempty"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// WSHandler handles the /api/v1/chat/ws WebSocket endpoint for bi-directional
// chat streaming. It implements bus.StreamDelegate so the agent loop can push
// tokens directly to the connected browser. Replaces the Wave 1 SSE handler
// per Wave 5a spec (non-behavior: must not use SSE for chat streaming).
type WSHandler struct {
	msgBus        *bus.MessageBus
	agentLoop     *agent.AgentLoop
	allowedOrigin string

	mu         sync.Mutex
	sessions   map[string]*wsConn  // chatID → connection
	sessionIDs map[string]string   // chatID → sessionID (for transcript recording)
	webchatCh  *webchatChannel     // reference to mark streaming complete

	// approvalRegistry tracks in-flight exec approval requests sent to the browser.
	// Shared across all connections on this handler; keyed by request UUID.
	// Although the registry is shared, each approval request is associated with the
	// connection that sent it — only that connection's browser tab can respond.
	approvalRegistry *wsApprovalRegistry

	upgrader websocket.Upgrader
}

type wsConn struct {
	conn          *websocket.Conn
	sendCh        chan []byte
	doneCh        chan struct{}
	closeOnce     sync.Once
	droppedTokens int
}

func (c *wsConn) close() {
	c.closeOnce.Do(func() { close(c.doneCh) })
}

// newWSHandler creates a WSHandler and registers it as the MessageBus stream delegate,
// replacing any previously registered delegate (e.g., the Wave 1 SSE handler).
func newWSHandler(
	msgBus *bus.MessageBus,
	agentLoop *agent.AgentLoop,
	allowedOrigin string,
) *WSHandler {
	h := &WSHandler{
		msgBus:           msgBus,
		agentLoop:        agentLoop,
		allowedOrigin:    allowedOrigin,
		sessions:         make(map[string]*wsConn),
		sessionIDs:       make(map[string]string),
		approvalRegistry: newWSApprovalRegistry(),
		upgrader: websocket.Upgrader{
			// CheckOrigin: parses the Origin URL and compares hostname against the request
			// Host to allow same-origin requests. Also allows localhost/127.0.0.1 for development.
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true // non-browser or same-origin
				}
				if allowedOrigin != "" && origin == allowedOrigin {
					return true
				}
				parsed, err := url.Parse(origin)
				if err != nil {
					return false
				}
				hostname := parsed.Hostname()
				originPort := parsed.Port()
				// Allow same-origin: Origin hostname+port matches the request Host.
				if r.Host != "" {
					hostOnly := r.Host
					hostPort := ""
					if h, p, err := net.SplitHostPort(r.Host); err == nil {
						hostOnly = h
						hostPort = p
					}
					if hostname == hostOnly && originPort == hostPort {
						return true
					}
				}
				// Allow localhost and loopback for development.
				return hostname == "localhost" || hostname == "127.0.0.1"
			},
		},
	}
	// NOTE: Do NOT call msgBus.SetStreamDelegate(h) here.
	// The channel Manager is already set as the stream delegate (via manager.go:254).
	// atomic.Value panics if you store a different concrete type.
	// Instead, the channel Manager's GetStreamer will be extended to check for
	// webchat WebSocket connections. For now, webchat streaming goes through
	// the Manager → Pico channel path, and we handle it via direct message publishing.
	return h
}

// GetStreamer implements bus.StreamDelegate.
// Returns a WebSocket streamer for webchat sessions that have an active connection.
func (h *WSHandler) GetStreamer(_ context.Context, channel, chatID string) (bus.Streamer, bool) {
	if channel != "webchat" {
		return nil, false
	}
	// Hold the lock for both map lookups to avoid a TOCTOU race where the
	// session could be removed between the two separate lock/unlock pairs.
	h.mu.Lock()
	conn, ok := h.sessions[chatID]
	sid := h.sessionIDs[chatID]
	h.mu.Unlock()
	if !ok {
		return nil, false
	}

	// Resolve the agent store for transcript recording.
	// The session is associated with a specific agent; look up that agent's store.
	// agentID is stored in the session meta — we use "main" as default for webchat.
	var agentStore *session.UnifiedStore
	if sid != "" {
		// Try to find which agent owns this session by scanning agent stores.
		agentStore = h.resolveSessionStore(sid)
	}

	return &wsStreamer{
		conn:       conn,
		chatID:     chatID,
		sessionID:  sid,
		agentStore: agentStore,
		channel:    h.webchatCh,
	}, true
}

// resolveSessionStore delegates to the shared AgentLoop method.
func (h *WSHandler) resolveSessionStore(sessionID string) *session.UnifiedStore {
	return h.agentLoop.ResolveSessionStore(sessionID)
}

// ServeHTTP handles the WebSocket upgrade and full connection lifecycle.
func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin := h.allowedOrigin
	if origin == "" {
		origin = "http://localhost:3000"
	}

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Upgrade, Connection, Sec-WebSocket-Key, Sec-WebSocket-Version")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "websocket upgrade required", http.StatusUpgradeRequired)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("ws: upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(_ string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	if !h.authenticateWS(conn) {
		return
	}

	chatID := "webchat:" + uuid.New().String()
	wc := &wsConn{
		conn:   conn,
		sendCh: make(chan []byte, 256),
		doneCh: make(chan struct{}),
	}

	h.mu.Lock()
	h.sessions[chatID] = wc
	h.mu.Unlock()

	// Mount a per-connection approval hook so the agent loop can request interactive
	// approval from this browser tab when a tool call requires user consent. The hook
	// sends an exec_approval_request frame and blocks until the browser responds or
	// the request times out.
	hookName := "ws-approval-" + chatID
	approvalHook := &wsApprovalHook{conn: wc, chatID: chatID, registry: h.approvalRegistry, timeout: wsApprovalTimeout}
	if err := h.agentLoop.MountHook(agent.NamedHook(hookName, approvalHook)); err != nil {
		slog.Error("ws: could not mount approval hook — closing connection", "chat_id", chatID, "error", err)
		sendConnFrame(wc, wsServerFrame{Type: "error", Message: "failed to initialize tool approval — please reconnect"})
		return
	}

	// Subscribe to agent-loop events so we can forward tool_call_start/result
	// frames to the browser in real time.
	eventSub := h.agentLoop.SubscribeEvents(32)
	eventDone := make(chan struct{})
	go h.eventForwarder(wc, chatID, eventSub, eventDone)

	defer func() {
		h.agentLoop.UnmountHook(hookName)
		h.agentLoop.UnsubscribeEvents(eventSub.ID)
		<-eventDone // wait for forwarder goroutine to exit
		h.mu.Lock()
		delete(h.sessions, chatID)
		delete(h.sessionIDs, chatID)
		h.mu.Unlock()
		wc.close()
	}()

	go h.writePump(wc)
	go h.pingPump(wc)

	var sessionID string
	h.readLoop(r.Context(), conn, wc, chatID, &sessionID)
}

// authenticateWS reads the first frame and validates the token if OMNIPUS_BEARER_TOKEN is set.
// Returns true if auth succeeds or is not required.
func (h *WSHandler) authenticateWS(conn *websocket.Conn) bool {
	required := os.Getenv("OMNIPUS_BEARER_TOKEN")
	if required == "" {
		return true
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		slog.Warn("ws: auth read failed", "error", err)
		return false
	}

	var frame wsClientFrame
	if err := json.Unmarshal(data, &frame); err != nil || frame.Type != "auth" {
		sendWSFrame(conn, wsServerFrame{Type: "error", Message: "first message must be {\"type\":\"auth\",\"token\":\"...\"}"})
		return false
	}

	if subtle.ConstantTimeCompare([]byte(frame.Token), []byte(required)) != 1 {
		sendWSFrame(conn, wsServerFrame{Type: "error", Message: "unauthorized: invalid token"})
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "authentication failed"))
		return false
	}

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	return true
}

// readLoop processes client frames until the connection closes.
func (h *WSHandler) readLoop(ctx context.Context, conn *websocket.Conn, wc *wsConn, chatID string, sessionID *string) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Debug("ws: connection closed unexpectedly", "chat_id", chatID, "error", err)
			}
			return
		}

		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			slog.Warn("ws: SetReadDeadline failed, closing connection", "chat_id", chatID, "error", err)
			return
		}

		var frame wsClientFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			slog.Warn("ws: malformed frame", "error", err)
			sendConnFrame(wc, wsServerFrame{Type: "error", Message: "malformed message frame"})
			continue
		}

		switch frame.Type {
		case "message":
			if frame.Content == "" {
				continue
			}
			h.handleChatMessage(ctx, chatID, sessionID, frame.Content, frame.AgentID, wc)
		case "cancel":
			h.handleCancel(sessionID)
		case "exec_approval_response":
			h.handleApprovalResponse(frame.ID, frame.Decision)
		case "attach_session":
			if frame.SessionID != "" {
				h.handleAttachSession(ctx, chatID, sessionID, frame.SessionID, wc)
			}
		case "ping":
			// Client heartbeat — no action needed, the WebSocket pong handler keeps the connection alive
		default:
			slog.Debug("ws: unknown frame type ignored", "type", frame.Type, "chat_id", chatID)
		}
	}
}

// handleChatMessage creates a session on the first message, records every user message,
// and publishes the message to the bus. If session creation fails, the client is warned
// that the conversation will not be persisted (fix for silent persistence failure).
func (h *WSHandler) handleChatMessage(ctx context.Context, chatID string, sessionID *string, content string, agentID string, wc *wsConn) {
	// Resolve the agent store to use. If agentID is provided, use that agent's store;
	// otherwise fall back to the main agent's store.
	targetAgentID := agentID
	if targetAgentID == "" {
		targetAgentID = "main"
	}
	store := h.agentLoop.GetAgentStore(targetAgentID)

	if store != nil {
		// Create the session on the first message of this WebSocket connection.
		if *sessionID == "" {
			meta, err := store.NewSession(session.SessionTypeChat, "webchat")
			if err != nil {
				slog.Warn("ws: could not create session — conversation will not be saved", "error", err)
				sendConnFrame(wc, wsServerFrame{
					Type:    "error",
					Message: "warning: could not create session — conversation will not be saved",
				})
				// Continue without sessionID so the message is still delivered to the agent.
			} else {
				*sessionID = meta.ID
				// Track sessionID for this chatID so the streamer can record responses.
				h.mu.Lock()
				h.sessionIDs[chatID] = meta.ID
				h.mu.Unlock()
				// Truncate using []rune so multi-byte UTF-8 characters aren't split.
				titleRunes := []rune(content)
				var title string
				if len(titleRunes) > 60 {
					title = string(titleRunes[:57]) + "..."
				} else {
					title = content
				}
				if err := store.SetMeta(meta.ID, session.MetaPatch{Title: &title}); err != nil {
					slog.Warn("ws: could not set session title", "session_id", meta.ID, "error", err)
				}
			}
		}

		// Record every user message to the transcript, not just the first one.
		if *sessionID != "" {
			entry := session.TranscriptEntry{
				ID:        uuid.New().String(),
				Role:      "user",
				Content:   content,
				Timestamp: time.Now().UTC(),
			}
			if err := store.AppendTranscript(*sessionID, entry); err != nil {
				slog.Warn("ws: could not record user message", "session_id", *sessionID, "error", err)
			}
		}
	}

	msg := bus.InboundMessage{
		Channel:  "webchat",
		SenderID: "webchat_user",
		ChatID:   chatID,
		Content:  content,
	}
	// Validate agent_id before embedding in metadata. An invalid ID is rejected
	// with an error frame — do NOT silently reroute to the default agent, which
	// would confuse the client about which agent is actually handling the message.
	if agentID != "" {
		if err := validateEntityID(agentID); err != nil {
			slog.Warn("ws: invalid agent_id in message frame; rejecting", "agent_id", agentID, "error", err)
			sendConnFrame(wc, wsServerFrame{Type: "error", Message: "invalid agent_id"})
			return
		}
		msg.Metadata = map[string]string{"agent_id": agentID}
	}
	pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := h.msgBus.PublishInbound(pubCtx, msg); err != nil {
		slog.Warn("ws: failed to publish message", "error", err)
		sendConnFrame(wc, wsServerFrame{Type: "error", Message: fmt.Sprintf("failed to deliver message: %v", err)})
	}
}

// handleCancel gracefully interrupts the current agent turn and marks the session as interrupted.
func (h *WSHandler) handleCancel(sessionID *string) {
	if err := h.agentLoop.InterruptGraceful("user cancelled via WebSocket"); err != nil {
		slog.Debug("ws: cancel — no active turn", "error", err)
	}
	if sessionID != nil && *sessionID != "" {
		store := h.resolveSessionStore(*sessionID)
		if store != nil {
			status := session.StatusInterrupted
			if err := store.SetMeta(*sessionID, session.MetaPatch{Status: &status}); err != nil {
				slog.Warn("ws: could not mark session interrupted", "session_id", *sessionID, "error", err)
			}
		}
	}
}

// handleAttachSession loads an existing session's transcript and replays it to the client,
// then sets the connection's active session to the requested session.
func (h *WSHandler) handleAttachSession(ctx context.Context, chatID string, sessionID *string, attachID string, wc *wsConn) {
	if err := validateEntityID(attachID); err != nil {
		sendConnFrame(wc, wsServerFrame{Type: "error", Message: "invalid session_id"})
		return
	}

	store := h.resolveSessionStore(attachID)
	if store == nil {
		sendConnFrame(wc, wsServerFrame{Type: "error", Message: "session not found"})
		return
	}

	entries, err := store.ReadTranscript(attachID)
	if err != nil {
		slog.Warn("ws: attach_session: could not read transcript", "session_id", attachID, "error", err)
		sendConnFrame(wc, wsServerFrame{Type: "error", Message: "could not read session transcript"})
		return
	}

	// Replay existing transcript entries as role-aware replay_message frames.
	for _, entry := range entries {
		if entry.Content != "" {
			data, merr := json.Marshal(wsServerFrame{
				Type:    "replay_message",
				Content: entry.Content,
				Role:    entry.Role,
			})
			if merr != nil {
				slog.Warn("ws: attach_session: could not marshal replay entry", "session_id", attachID, "error", merr)
				continue
			}
			select {
			case wc.sendCh <- data:
			case <-ctx.Done():
				return
			}
		}
	}

	sendConnFrame(wc, wsServerFrame{Type: "done", Stats: map[string]any{}})

	// Switch this connection's active session.
	*sessionID = attachID
	h.mu.Lock()
	h.sessionIDs[chatID] = attachID
	h.mu.Unlock()

	slog.Debug("ws: attached to session", "chat_id", chatID, "session_id", attachID)
}

// handleApprovalResponse resolves a pending exec approval request.
// Called from readLoop when the browser sends an "exec_approval_response" frame.
// "allow" maps to VerdictAllow, "always" maps to VerdictAlways, everything else maps to VerdictDeny.
func (h *WSHandler) handleApprovalResponse(id, decision string) {
	if id == "" {
		slog.Warn("ws: exec_approval_response missing id")
		return
	}
	var verdict agent.ApprovalVerdict
	switch decision {
	case "allow":
		verdict = agent.VerdictAllow
	case "always":
		verdict = agent.VerdictAlways
	default:
		verdict = agent.VerdictDeny
	}
	d := agent.ApprovalDecision{Verdict: verdict}
	if verdict == agent.VerdictDeny {
		d.Reason = "user denied via WebSocket"
	}
	if !h.approvalRegistry.resolve(id, d) {
		// The request may have already timed out — this is informational, not an error.
		slog.Debug("ws: exec_approval_response for unknown or expired request", "id", id, "decision", decision)
	} else {
		slog.Info("ws: exec_approval resolved", "id", id, "decision", decision, "verdict", verdict)
	}
}

// wsPingMsg is a nil sentinel enqueued by pingPump to signal writePump to send a WebSocket ping.
// Using a sentinel through sendCh ensures all writes go through the single writer goroutine,
// satisfying gorilla/websocket's single-writer requirement (fix for gorilla write race).
// Important: do not pass nil []byte through sendCh for any other purpose — nil is reserved as the ping sentinel.
var wsPingMsg []byte

// writePump is the single goroutine that writes all frames to the WebSocket connection.
// gorilla/websocket requires all writes to happen from the same goroutine.
// A nil message on sendCh is the sentinel for a ping frame.
func (h *WSHandler) writePump(wc *wsConn) {
	for {
		select {
		case msg, ok := <-wc.sendCh:
			if !ok {
				return
			}
			if msg == nil {
				// nil sentinel: send a WebSocket ping frame.
				if err := wc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					slog.Debug("ws: ping write error", "error", err)
					return
				}
				continue
			}
			if err := wc.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Debug("ws: write error", "error", err)
				return
			}
		case <-wc.doneCh:
			return
		}
	}
}

// pingPump enqueues a nil sentinel onto sendCh every 30 s for keep-alive pings.
// All writes go through writePump, satisfying gorilla's single-writer requirement.
func (h *WSHandler) pingPump(wc *wsConn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			select {
			case wc.sendCh <- wsPingMsg: // nil sentinel triggers a ping in writePump
			case <-wc.doneCh:
				return
			}
		case <-wc.doneCh:
			return
		}
	}
}

// sendConnFrame marshals a frame and enqueues it on wc's send channel.
// For "done" and "error" frames, blocks up to 5 s rather than dropping, because
// losing these frames would leave the client in a permanently stuck state.
func sendConnFrame(wc *wsConn, frame wsServerFrame) {
	data, err := json.Marshal(frame)
	if err != nil {
		slog.Error("ws: marshal frame failed", "error", err)
		return
	}
	switch frame.Type {
	case "done", "error", "exec_approval_request", "exec_approval_expired":
		// Critical frames must not be dropped. Block briefly; force-close on timeout.
		// Approval frames are critical: dropping them leaves the agent turn blocked for
		// the full approval timeout (90 s) and then results in a mysterious denial.
		select {
		case wc.sendCh <- data:
		case <-time.After(5 * time.Second):
			slog.Warn("ws: send channel full after timeout for critical frame, closing connection", "type", frame.Type)
			wc.close()
		}
	default:
		select {
		case wc.sendCh <- data:
		default:
			slog.Warn("ws: send channel full, frame dropped", "type", frame.Type)
			wc.droppedTokens++
		}
	}
}

// sendWSFrame writes a frame directly to a connection (used before the send goroutine starts).
func sendWSFrame(conn *websocket.Conn, frame wsServerFrame) {
	data, err := json.Marshal(frame)
	if err != nil {
		slog.Error("ws: marshal frame failed", "error", err)
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		slog.Debug("ws: write frame failed", "type", frame.Type, "error", err)
	}
}

// eventForwarder listens on the agent EventBus and forwards tool_call_start/result
// frames to the browser so tool call UIs render in real time.
func (h *WSHandler) eventForwarder(wc *wsConn, chatID string, sub agent.EventSubscription, done chan<- struct{}) {
	defer close(done)
	for evt := range sub.C {
		switch evt.Kind {
		case agent.EventKindToolExecStart:
			p, ok := evt.Payload.(agent.ToolExecStartPayload)
			if !ok || p.ChatID != chatID {
				continue
			}
			sendConnFrame(wc, wsServerFrame{
				Type:   "tool_call_start",
				CallID: p.ToolCallID,
				Tool:   p.Tool,
				Params: p.Arguments,
			})
		case agent.EventKindToolExecEnd:
			p, ok := evt.Payload.(agent.ToolExecEndPayload)
			if !ok || p.ChatID != chatID {
				continue
			}
			status := "success"
			if p.IsError {
				status = "error"
			}
			sendConnFrame(wc, wsServerFrame{
				Type:       "tool_call_result",
				CallID:     p.ToolCallID,
				Tool:       p.Tool,
				Status:     status,
				DurationMs: p.Duration.Milliseconds(),
			})
		}
	}
}

// wsStreamer implements bus.Streamer, pushing token/done frames into a wsConn's send channel.
// It also accumulates the full response to persist it to the session transcript on Finalize.
type wsStreamer struct {
	conn        *wsConn
	chatID      string
	sessionID   string                   // for recording assistant message
	agentStore  *session.UnifiedStore    // for recording assistant message
	channel     *webchatChannel          // to mark streaming complete and suppress duplicate Send()
	accumulated strings.Builder          // accumulates full response text
}

func (s *wsStreamer) Update(_ context.Context, content string) error {
	data, err := json.Marshal(wsServerFrame{Type: "token", Content: content})
	if err != nil {
		return fmt.Errorf("ws: marshal token frame: %w", err)
	}
	select {
	case s.conn.sendCh <- data:
		s.accumulated.WriteString(content)
		return nil
	default:
		slog.Warn("ws: token dropped — client send buffer full")
		return fmt.Errorf("ws: token channel full, token dropped")
	}
}

func (s *wsStreamer) Finalize(_ context.Context, _ string) error {
	stats := map[string]any{}
	if s.conn.droppedTokens > 0 {
		stats["tokens_dropped"] = s.conn.droppedTokens
	}
	sendConnFrame(s.conn, wsServerFrame{Type: "done", Stats: stats})
	// Mark this chatID as streamed so webchatChannel.Send() skips the duplicate.
	if s.channel != nil {
		s.channel.markStreamed(s.chatID)
	}
	// Record the full assistant response to the session transcript.
	if s.agentStore != nil && s.sessionID != "" {
		content := s.accumulated.String()
		if content != "" {
			entry := session.TranscriptEntry{
				ID:        uuid.New().String(),
				Role:      "assistant",
				Content:   content,
				Timestamp: time.Now().UTC(),
			}
			if err := s.agentStore.AppendTranscript(s.sessionID, entry); err != nil {
				slog.Warn("ws: could not record streamed assistant message", "session_id", s.sessionID, "error", err)
			}
		}
	}
	return nil
}

func (s *wsStreamer) Cancel(_ context.Context) {
	s.conn.close()
}
