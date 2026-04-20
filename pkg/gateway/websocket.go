//go:build !cgo

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
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"

	"github.com/dapicom-ai/omnipus/pkg/agent"
	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/pairing"
	"github.com/dapicom-ai/omnipus/pkg/session"
)

// replayLiveBufferCap is the maximum number of live WS frames buffered during
// replay (FR-I-009). Frames beyond this cap are dropped under backpressure.
const replayLiveBufferCap = 1000

// wsClientFrame is a message sent from the browser to the server over WebSocket.
type wsClientFrame struct {
	Type      string `json:"type"`                 // "auth" | "message" | "cancel" | "exec_approval_response" | "attach_session" | "device_pairing_response"
	Token     string `json:"token,omitempty"`      // for "auth"
	Content   string `json:"content,omitempty"`    // for "message"
	SessionID string `json:"session_id,omitempty"` // for "message" / "cancel" / "attach_session"
	AgentID   string `json:"agent_id,omitempty"`   // for "message" — route to specific agent
	ID        string `json:"id,omitempty"`         // for "exec_approval_response"
	Decision  string `json:"decision,omitempty"`   // "allow" | "deny" | "always" for exec; "approve" | "reject" for device_pairing_response
	DeviceID  string `json:"device_id,omitempty"`  // for "device_pairing_response"
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
	// device_pairing_request fields
	DeviceID    string `json:"device_id,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	PairingCode string `json:"pairing_code,omitempty"`
	DeviceName  string `json:"device_name,omitempty"`
	// rate_limit fields (SEC-26)
	Scope             string  `json:"scope,omitempty"`
	Resource          string  `json:"resource,omitempty"`
	PolicyRule        string  `json:"policy_rule,omitempty"`
	RetryAfterSeconds float64 `json:"retry_after_seconds,omitempty"`
	AgentID           string  `json:"agent_id,omitempty"`
	// media frame fields
	Parts []wsMediaPart `json:"parts,omitempty"`
	// subagent span fields (FR-H-004, FR-H-005)
	// SpanID is "span_" + parent spawn ToolCall.ID. Present on subagent_start and subagent_end.
	SpanID string `json:"span_id,omitempty"`
	// ParentCallID is the parent spawn ToolCall.ID. Present on subagent_start, subagent_end,
	// and tool_call_start/result frames fired inside a sub-turn. Empty for top-level calls.
	ParentCallID string `json:"parent_call_id,omitempty"`
	// TaskLabel is the human-readable label for the subagent task (subagent_start only).
	TaskLabel string `json:"task_label,omitempty"`
}

// wsMediaPart represents a single media attachment in a "media" WebSocket frame.
type wsMediaPart struct {
	Type        string `json:"type"`         // "image" | "audio" | "video" | "file"
	URL         string `json:"url"`          // /api/v1/media/{ref}
	Filename    string `json:"filename"`     // original filename
	ContentType string `json:"content_type"` // MIME type
	Caption     string `json:"caption,omitempty"`
}

// WSHandler handles the /api/v1/chat/ws WebSocket endpoint for bi-directional
// chat streaming. It implements bus.StreamDelegate so the agent loop can push
// tokens directly to the connected browser. Replaces the Wave 1 SSE handler
// per Wave 5a spec (non-behavior: must not use SSE for chat streaming).
type WSHandler struct {
	msgBus        *bus.MessageBus
	agentLoop     *agent.AgentLoop
	allowedOrigin string

	// activeConns tracks in-flight ServeHTTP goroutines so Wait() can block
	// until all connections have fully torn down (used by tests to avoid
	// tempdir cleanup races).
	activeConns sync.WaitGroup

	mu          sync.Mutex
	sessions    map[string]*wsConn // chatID → connection
	sessionIDs  map[string]string  // chatID → sessionID (for transcript recording)
	taskChatIDs map[string]string  // browser chatID → task chatID for live event forwarding
	webchatCh   *webchatChannel    // reference to mark streaming complete

	// approvalRegistry tracks in-flight exec approval requests sent to the browser.
	// Shared across all connections on this handler; keyed by request UUID.
	// Although the registry is shared, each approval request is associated with the
	// connection that sent it — only that connection's browser tab can respond.
	approvalRegistry *wsApprovalRegistry

	// devicePairingRegistry tracks in-flight device pairing requests awaiting admin approval.
	devicePairingRegistry *devicePairingRegistry

	// pairingStore is the global device pairing state (pending + paired devices).
	pairingStore *pairing.PairingStore

	upgrader websocket.Upgrader
}

type wsConn struct {
	conn          *websocket.Conn
	sendCh        chan []byte
	doneCh        chan struct{}
	closeOnce     sync.Once
	droppedTokens atomic.Int32
	droppedFrames atomic.Int32    // non-critical frames dropped due to backpressure
	role          config.UserRole // RBAC role resolved at auth time

	// Replay-mode divert (W1-1): during replay, live events arriving via
	// sendConnFrame are redirected into replayDivertCh instead of sendCh so
	// they don't interleave with replay frames. After replay finishes they are
	// drained into sendCh in arrival order.
	// isReplayingLive is set atomically before replay starts and cleared after
	// the done frame is sent, so concurrent callers of sendConnFrame see a
	// consistent view without holding any mutex.
	isReplayingLive atomic.Bool
	replayDivertCh  chan []byte // capacity replayLiveBufferCap; allocated lazily by handleAttachSession
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
		msgBus:                msgBus,
		agentLoop:             agentLoop,
		allowedOrigin:         allowedOrigin,
		sessions:              make(map[string]*wsConn),
		sessionIDs:            make(map[string]string),
		taskChatIDs:           make(map[string]string),
		approvalRegistry:      newWSApprovalRegistry(),
		devicePairingRegistry: newDevicePairingRegistry(),
		pairingStore:          pairing.NewPairingStore(),
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
				// Allow localhost and loopback for development ONLY when no explicit origin is configured.
				return allowedOrigin == "" && (hostname == "localhost" || hostname == "127.0.0.1")
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
	var agentStore *session.UnifiedStore
	if sid != "" {
		// Try to find which agent owns this session by scanning agent stores.
		agentStore = h.resolveSessionStore(sid)
	}

	// Resolve the active agent for this chat session so the transcript entry
	// can be tagged with the correct agent ID (FR-002).
	activeAgentID := ""
	if aid, ok := h.agentLoop.GetSessionActiveAgent(chatID); ok {
		activeAgentID = aid
	}

	return &wsStreamer{
		conn:       conn,
		chatID:     chatID,
		sessionID:  sid,
		agentStore: agentStore,
		agentID:    activeAgentID,
		channel:    h.webchatCh,
	}, true
}

// resolveSessionStore delegates to the shared AgentLoop method.
func (h *WSHandler) resolveSessionStore(sessionID string) *session.UnifiedStore {
	return h.agentLoop.ResolveSessionStore(sessionID)
}

// Wait blocks until all active ServeHTTP goroutines have fully exited.
// Call this in test cleanup (after srv.Close()) to prevent tempdir removal
// races with background session writes.
func (h *WSHandler) Wait() {
	h.activeConns.Wait()
}

// ServeHTTP handles the WebSocket upgrade and full connection lifecycle.
func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.activeConns.Add(1)
	defer h.activeConns.Done()

	origin := h.allowedOrigin
	if origin == "" {
		origin = "http://localhost:3000"
	}

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().
			Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Upgrade, Connection, Sec-WebSocket-Key, Sec-WebSocket-Version")
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

	// Create wsConn before auth so authenticateWS can set the role on it.
	wc := &wsConn{
		conn:   conn,
		sendCh: make(chan []byte, 256),
		doneCh: make(chan struct{}),
	}

	if !h.authenticateWS(conn, wc) {
		return
	}

	chatID := "webchat:" + uuid.New().String()

	h.mu.Lock()
	h.sessions[chatID] = wc
	h.mu.Unlock()

	// Mount a per-connection approval hook so the agent loop can request interactive
	// approval from this browser tab when a tool call requires user consent. The hook
	// sends an exec_approval_request frame and blocks until the browser responds or
	// the request times out.
	hookName := "ws-approval-" + chatID
	approvalHook := &wsApprovalHook{
		conn:     wc,
		chatID:   chatID,
		registry: h.approvalRegistry,
		timeout:  wsApprovalTimeout,
		policyResolver: func(toolName string, agentID string) string {
			cfg := h.agentLoop.GetConfig()
			// Global policy (floor) — derived from sandbox config.
			globalPolicy := "allow"
			if p, ok := cfg.Sandbox.ToolPolicies[toolName]; ok {
				globalPolicy = p
			} else if cfg.Sandbox.DefaultToolPolicy != "" {
				globalPolicy = cfg.Sandbox.DefaultToolPolicy
			}
			// Agent-level policy.
			agentPolicy := "allow"
			for _, ac := range cfg.Agents.List {
				if ac.ID == agentID && ac.Tools != nil {
					agentPolicy = string(ac.Tools.Builtin.ResolvePolicy(toolName))
					break
				}
			}
			// Strictest wins: deny > ask > allow.
			return resolveEffectivePolicy(globalPolicy, agentPolicy)
		},
	}
	if err := h.agentLoop.MountHook(agent.NamedHook(hookName, approvalHook)); err != nil {
		slog.Error("ws: could not mount approval hook — closing connection", "chat_id", chatID, "error", err)
		sendConnFrame(
			wc,
			wsServerFrame{Type: "error", Message: "failed to initialize tool approval — please reconnect"},
		)
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
		if tid, ok := h.taskChatIDs[chatID]; ok {
			delete(h.sessions, tid)
			delete(h.sessionIDs, tid)
			delete(h.taskChatIDs, chatID)
		}
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

// authenticateWS reads the first frame and validates the token.
// Supports RBAC: checks config.Gateway.Users first (bcrypt), then falls back to
// OMNIPUS_BEARER_TOKEN env var for backward compatibility.
// Sets wc.role to the resolved role on success.
func (h *WSHandler) authenticateWS(conn *websocket.Conn, wc *wsConn) bool {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		slog.Warn("ws: auth read failed", "error", err)
		return false
	}

	var frame wsClientFrame
	if err := json.Unmarshal(data, &frame); err != nil || frame.Type != "auth" {
		sendWSFrame(
			conn,
			wsServerFrame{Type: "error", Message: "first message must be {\"type\":\"auth\",\"token\":\"...\"}"},
		)
		return false
	}

	cfg := h.agentLoop.GetConfig()
	rawToken := frame.Token

	// 1. Check per-user list (RBAC — bcrypt token hash lookup).
	if len(cfg.Gateway.Users) > 0 {
		for _, user := range cfg.Gateway.Users {
			if err := bcrypt.CompareHashAndPassword([]byte(user.TokenHash), []byte(rawToken)); err == nil {
				wc.role = user.Role
				conn.SetReadDeadline(time.Now().Add(60 * time.Second))
				return true
			}
		}
		// Token not in user list — reject.
		sendWSFrame(conn, wsServerFrame{Type: "error", Message: "unauthorized: invalid token"})
		conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "authentication failed"),
		)
		return false
	}

	// 2. Fallback: legacy OMNIPUS_BEARER_TOKEN env var (treated as admin role).
	required := os.Getenv("OMNIPUS_BEARER_TOKEN")
	if required == "" {
		if cfg.Gateway.DevModeBypass {
			// Dev mode: allow without auth, treated as admin.
			wc.role = config.UserRoleAdmin
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return true
		}
		// No auth configured — deny by default (fail closed), matching HTTP auth path.
		sendWSFrame(conn, wsServerFrame{Type: "error", Message: "no users configured, complete onboarding first"})
		conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "authentication failed"),
		)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(rawToken), []byte(required)) != 1 {
		sendWSFrame(conn, wsServerFrame{Type: "error", Message: "unauthorized: invalid token"})
		conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "authentication failed"),
		)
		return false
	}
	wc.role = config.UserRoleAdmin
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	return true
}

// wsMaxMessageBytes is the maximum size of an incoming WebSocket message (5 MB).
// Messages exceeding this limit are rejected with an error frame and the connection
// is closed by gorilla/websocket (SetReadLimit causes a protocol-level close).
const wsMaxMessageBytes = 5 * 1024 * 1024

// readLoop processes client frames until the connection closes.
func (h *WSHandler) readLoop(ctx context.Context, conn *websocket.Conn, wc *wsConn, chatID string, sessionID *string) {
	// Enforce a hard read limit so clients cannot exhaust server memory with
	// oversized frames. gorilla/websocket will return an error on the next
	// ReadMessage call if the incoming frame exceeds this limit.
	conn.SetReadLimit(wsMaxMessageBytes)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			// gorilla/websocket returns CloseMessageTooBig (1009) when a frame exceeds
			// SetReadLimit. Notify the client with a human-readable error frame before
			// the connection is torn down (the write may silently fail if already closed,
			// which is acceptable — we make a best-effort attempt).
			if websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
				slog.Warn(
					"ws: message too large, closing connection",
					"chat_id",
					chatID,
					"limit_bytes",
					wsMaxMessageBytes,
				)
				sendWSFrame(conn, wsServerFrame{Type: "error", Message: "message too large (max 5MB)"})
				return
			}
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Debug("ws: connection closed unexpectedly", "chat_id", chatID, "error", err)
			}
			return
		}

		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			slog.Warn("ws: SetReadDeadline failed, exiting readLoop", "chat_id", chatID, "error", err)
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
			slog.Info("ws: attach_session frame received",
				"chat_id", chatID,
				"requested_session_id", frame.SessionID,
			)
			if frame.SessionID != "" {
				h.handleAttachSession(ctx, chatID, sessionID, frame.SessionID, wc)
			} else {
				slog.Warn("ws: attach_session with empty session_id", "chat_id", chatID)
			}
		case "ping":
			// Client heartbeat — no action needed, the WebSocket pong handler keeps the connection alive
		case "device_pairing_response":
			h.handleDevicePairingResponse(frame.DeviceID, frame.Decision)
		default:
			slog.Debug("ws: unknown frame type ignored", "type", frame.Type, "chat_id", chatID)
		}
	}
}

// handleChatMessage creates a session on the first message, records every user message,
// and publishes the message to the bus. If session creation fails, the client is warned
// that the conversation will not be persisted (fix for silent persistence failure).
func (h *WSHandler) handleChatMessage(
	ctx context.Context,
	chatID string,
	sessionID *string,
	content string,
	agentID string,
	wc *wsConn,
) {
	// Resolve the creating agent ID. If agentID is provided, use it;
	// otherwise fall back to the main agent.
	targetAgentID := agentID
	if targetAgentID == "" {
		targetAgentID = "main"
	}
	// Use the shared session store for new sessions (joined session model).
	// Fall back to the per-agent store if the shared store is unavailable.
	store := h.agentLoop.GetSessionStore()
	if store == nil {
		store = h.agentLoop.GetAgentStore(targetAgentID)
	}

	if store != nil {
		// Create the session on the first message of this WebSocket connection.
		if *sessionID == "" {
			meta, err := store.NewSession(session.SessionTypeChat, "webchat", targetAgentID)
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
		// AgentID on user entries identifies the agent the message was directed to.
		if *sessionID != "" {
			entry := session.TranscriptEntry{
				ID:        uuid.New().String(),
				Role:      "user",
				AgentID:   targetAgentID,
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
	// Embed the session ID in metadata so the agent loop can record tool calls
	// to the transcript for later replay via attach_session.
	if *sessionID != "" {
		if msg.Metadata == nil {
			msg.Metadata = make(map[string]string)
		}
		msg.Metadata["transcript_session_id"] = *sessionID
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
	if err := h.agentLoop.InterruptGraceful("user canceled via WebSocket"); err != nil {
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

// handleAttachSession loads an existing session's transcript and replays it to
// the client via streamReplay, then sets the connection's active session to the
// requested session.
//
// FR-I-009: the connection is registered for live-event forwarding BEFORE the
// replay starts. Live events arriving during replay are buffered in a capped
// channel; after the done frame is emitted the buffer is drained to the WS in
// arrival order.
func (h *WSHandler) handleAttachSession(
	ctx context.Context,
	chatID string,
	sessionID *string,
	attachID string,
	wc *wsConn,
) {
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

	rs := computeReplayStats(entries)

	// FR-I-013: structured log at replay start.
	// W3-2: include orphan/duplicate/truncated counts so the replay_start log
	// line carries enough context to debug fidelity issues without replay_end.
	slog.Info("ws: replay_start",
		"event", "replay_start",
		"session_id", attachID,
		"entry_count_loaded", len(entries),
		"tool_call_count_loaded", rs.toolCallCount,
		"span_count_detected", rs.spanCount,
		"orphan_count", rs.orphanCount,
		"duplicate_tool_call_id_count", rs.duplicateToolCallIDCount,
		"truncated_result_count", rs.truncatedResultCount,
	)
	replayStart := time.Now()

	// FR-I-009 / W1-1: register for live-event forwarding BEFORE starting replay
	// so no live events are lost during the replay window.
	//
	// Live events arriving via sendConnFrame during replay are diverted into
	// wc.replayDivertCh (allocated below) by the atomic flag wc.isReplayingLive.
	// writePump drains wc.sendCh as normal — replay frames go there directly.
	// After the done frame, the flag is cleared and the divert buffer is drained
	// into wc.sendCh in arrival order.
	//
	// This replaces the previous wc.sendCh swap which caused a data race because
	// writePump and pingPump read wc.sendCh concurrently with no synchronisation.
	if wc.replayDivertCh == nil {
		wc.replayDivertCh = make(chan []byte, replayLiveBufferCap)
	}

	// Register for live event forwarding now (before flipping the replay flag).
	h.mu.Lock()
	if oldTID, ok := h.taskChatIDs[chatID]; ok {
		delete(h.sessions, oldTID)
		delete(h.sessionIDs, oldTID)
	}
	h.taskChatIDs[chatID] = attachID
	h.sessions[attachID] = wc
	h.sessionIDs[attachID] = attachID
	h.mu.Unlock()

	// Arm the divert: any sendConnFrame calls after this point will route live
	// frames into replayDivertCh instead of sendCh.
	wc.isReplayingLive.Store(true)

	// Run replay: emit frames directly into wc.sendCh via emitFn, bypassing the
	// divert.  W1-10: a per-frame 5 s timeout prevents indefinite blocking when
	// the client is not draining the socket.
	emitFn := func(f wsServerFrame) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		data, merr := json.Marshal(f)
		if merr != nil {
			return merr
		}
		select {
		case wc.sendCh <- data:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return errSendTimeout
		}
	}

	// W3-3: pass pre-computed rs into streamReplay so it doesn't rebuild
	// spawnIDsWithChildren for a second time.
	framesEmitted, replayErr := streamReplay(ctx, attachID, entries, rs, emitFn)

	// Disarm the divert FIRST so that subsequent sendConnFrame calls go directly
	// to sendCh once we drain the buffer below.
	wc.isReplayingLive.Store(false)

	durationMS := time.Since(replayStart).Milliseconds()

	if replayErr != nil {
		slog.Warn("ws: replay_aborted",
			"event", "replay_aborted",
			"session_id", attachID,
			"frames_emitted", framesEmitted,
			"duration_ms", durationMS,
			"error", replayErr,
		)
		// W1-5: emit error + synthetic done so the client clears isReplaying and
		// re-enables the composer.  Use sendConnFrame (canonical path) because the
		// divert flag is already cleared above.
		sendConnFrame(wc, wsServerFrame{
			Type:    "error",
			Message: "replay aborted: " + replayErr.Error(),
		})
		sendConnFrame(wc, wsServerFrame{
			Type:  "done",
			Stats: map[string]any{"replay_error": true},
		})
		return
	}

	// FR-I-013: structured log at replay end.
	// W3-2: include the full stats set so replay_end is a self-contained diagnostic record.
	slog.Info("ws: replay_end",
		"event", "replay_end",
		"session_id", attachID,
		"frames_emitted", framesEmitted,
		"duration_ms", durationMS,
		"orphan_count", rs.orphanCount,
		"duplicate_tool_call_id_count", rs.duplicateToolCallIDCount,
		"truncated_result_count", rs.truncatedResultCount,
	)

	// FR-I-009: drain any live events buffered during replay, in arrival order.
	// The divert flag is already cleared, so no new frames will land here.
	// We drain until the buffer is empty (non-blocking).
	for {
		select {
		case raw := <-wc.replayDivertCh:
			select {
			case wc.sendCh <- raw:
			case <-ctx.Done():
				return
			}
		default:
			goto drainDone
		}
	}
drainDone:

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

// errSendTimeout is returned by the replay emitFn when the send channel is
// full for more than 5 seconds (W1-10). The caller (streamReplay) surfaces
// this via W1-5's error+done emission so the client can recover.
var errSendTimeout = fmt.Errorf("ws: send channel full — replay send timeout")

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
// For "done", "error", and approval frames, blocks up to 5 s rather than dropping,
// because losing these frames would leave the client in a permanently stuck state.
// For non-critical frames, retries with short delays (immediate, 10ms, 50ms) before dropping.
// After 20 cumulative dropped frames a "degraded" error frame is
// injected into the critical path to warn the client; the counter resets on success.
//
// During replay (wc.isReplayingLive == true), live frames arriving from the
// eventForwarder are diverted into wc.replayDivertCh so they do not interleave
// with replay frames that are being written directly to wc.sendCh. After replay
// finishes, handleAttachSession drains replayDivertCh into sendCh in order.
// This replaces the old wc.sendCh swap which caused a data race.
//
// droppedFramesWarnThreshold is the number of consecutively dropped non-critical
// frames after which a "connection degraded" error is sent to the browser.
const droppedFramesWarnThreshold = 20

func sendConnFrame(wc *wsConn, frame wsServerFrame) {
	data, err := json.Marshal(frame)
	if err != nil {
		slog.Error("ws: marshal frame failed", "error", err)
		return
	}

	// W1-1: if replay mode is active, divert live frames into the replay buffer
	// instead of wc.sendCh, so writePump never sees them while replay is running.
	// "done", "error", and critical control frames are always sent to the canonical
	// sendCh regardless of replay state — they are emitted by streamReplay itself
	// and must reach writePump immediately.
	targetCh := wc.sendCh
	isCritical := frame.Type == "done" || frame.Type == "error" ||
		frame.Type == "exec_approval_request" || frame.Type == "exec_approval_expired"
	if !isCritical && wc.isReplayingLive.Load() && wc.replayDivertCh != nil {
		targetCh = wc.replayDivertCh
	}

	switch {
	case isCritical:
		// Critical frames must not be dropped. Block briefly; force-close on timeout.
		// Approval frames are critical: dropping them leaves the agent turn blocked for
		// the full approval timeout (90 s) and then results in a mysterious denial.
		select {
		case targetCh <- data:
		case <-time.After(5 * time.Second):
			slog.Warn("ws: send channel full after timeout for critical frame, closing connection", "type", frame.Type)
			wc.close()
		}
	default:
		// Try immediate send, then graduated retry delays (10 ms, 50 ms) before dropping.
		backoffs := [...]time.Duration{0, 10 * time.Millisecond, 50 * time.Millisecond}
		for _, wait := range backoffs {
			if wait == 0 {
				select {
				case targetCh <- data:
					wc.droppedFrames.Store(0)
					return
				default:
				}
			} else {
				t := time.NewTimer(wait)
				select {
				case targetCh <- data:
					t.Stop()
					wc.droppedFrames.Store(0)
					return
				case <-t.C:
					// Timer expired, try next delay.
				}
			}
		}

		// All attempts exhausted — drop the frame and record backpressure.
		slog.Warn("ws: send channel full after backoff, frame dropped", "type", frame.Type)
		wc.droppedTokens.Add(1)
		wc.droppedFrames.Add(1)

		// After threshold drops, warn the client over the critical path so it knows
		// the connection is degraded. The degraded warning always goes to the canonical
		// wc.sendCh — never to replayDivertCh — so the user sees the overflow warning
		// immediately without waiting for replay to drain (W1-6).
		if wc.droppedFrames.Load() >= int32(droppedFramesWarnThreshold) {
			wc.droppedFrames.Store(0)
			degraded, merr := json.Marshal(wsServerFrame{
				Type:    "error",
				Message: "connection degraded: frames being dropped due to backpressure",
			})
			if merr != nil {
				slog.Error("ws: marshal degraded frame failed", "error", merr)
				return
			}
			select {
			case wc.sendCh <- degraded:
			case <-time.After(5 * time.Second):
				slog.Warn("ws: could not deliver degraded warning frame, closing connection")
				wc.close()
			}
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

// orphanWatchdogTimeout is the duration the forwarder waits after a parent turn ends
// before synthesizing a subagent_end{status:"interrupted"} for any still-open span.
// Configurable so tests can override to a short value (e.g., 200ms) without sleeping.
// Production default is 5 seconds (FR-H-004 / Scenario 7).
var orphanWatchdogTimeout = 5 * time.Second

// openSpanEntry tracks an in-flight subagent span in the event forwarder.
type openSpanEntry struct {
	spanID        string
	parentCallID  string
	agentID       string
	parentTurnEnded bool       // set to true when EventKindTurnEnd fires for the parent turn
	closeCh       chan struct{} // closed when EventKindSubTurnEnd arrives (cancels watchdog)
}

// eventForwarder listens on the agent EventBus and forwards tool_call_start/result
// frames to the browser so tool call UIs render in real time.
// It also matches events from an attached task session (via taskChatIDs).
// Extended (FR-H-004, FR-H-005): emits subagent_start / subagent_end frames and
// propagates parent_call_id on tool_call_* frames fired inside sub-turns.
// Orphan watchdog (FR-H-004, Scenario 7): when the parent turn ends before all spans
// are closed, a timer fires after orphanWatchdogTimeout and synthesizes
// subagent_end{status:"interrupted"} for each still-open span.
func (h *WSHandler) eventForwarder(wc *wsConn, chatID string, sub agent.EventSubscription, done chan<- struct{}) {
	defer close(done)

	// matchesChatID returns true if evtChatID belongs to this connection's chat or
	// to a task session the connection has attached to via handleAttachSession.
	matchesChatID := func(evtChatID string) bool {
		if evtChatID == chatID {
			return true
		}
		h.mu.Lock()
		tid := h.taskChatIDs[chatID]
		h.mu.Unlock()
		// Note: using exclusive lock for a read-only lookup. Acceptable for now;
		// migrate h.mu to sync.RWMutex if contention becomes measurable.
		return tid != "" && evtChatID == tid
	}

	// openSpans tracks in-flight subagent spans keyed by parentCallID.
	// Accessed only from the single eventForwarder goroutine — no mutex needed.
	openSpans := make(map[string]*openSpanEntry)

	// closeSpan marks a span as resolved and signals its watchdog to stop.
	closeSpan := func(parentCallID string) {
		if entry, ok := openSpans[parentCallID]; ok {
			select {
			case <-entry.closeCh: // already closed
			default:
				close(entry.closeCh)
			}
			delete(openSpans, parentCallID)
		}
	}

	// startOrphanWatchdog launches a goroutine that fires after orphanWatchdogTimeout
	// if the span is not closed first. On timeout it synthesizes subagent_end and logs.
	// W1-9: the goroutine also exits cleanly when wc.doneCh is closed (connection torn down).
	startOrphanWatchdog := func(entry *openSpanEntry, reason string) {
		go func() {
			select {
			case <-entry.closeCh:
				// Span resolved normally — nothing to do.
				return
			case <-wc.doneCh:
				// Connection closed while waiting — exit cleanly without emitting.
				return
			case <-time.After(orphanWatchdogTimeout):
				// Span is still open after timeout. Emit interrupted.
				if reason == "unknown" {
					slog.Error("ws: subagent span orphaned with unknown reason — synthesizing interrupted end",
						"event", "span_orphan_interrupted",
						"span_id", entry.spanID,
						"parent_call_id", entry.parentCallID,
						"reason", reason,
					)
				} else {
					slog.Warn("ws: subagent span orphaned — synthesizing interrupted end",
						"event", "span_orphan_interrupted",
						"span_id", entry.spanID,
						"parent_call_id", entry.parentCallID,
						"reason", reason,
					)
				}
				sendConnFrame(wc, wsServerFrame{
					Type:         "subagent_end",
					SpanID:       entry.spanID,
					ParentCallID: entry.parentCallID,
					AgentID:      entry.agentID,
					Status:       "interrupted",
					Message:      reason,
				})
			}
		}()
	}

	for evt := range sub.C {
		switch evt.Kind {
		case agent.EventKindSubTurnSpawn:
			// FR-H-004: emit subagent_start when a sub-turn is spawned.
			p, ok := evt.Payload.(agent.SubTurnSpawnPayload)
			if !ok || !matchesChatID(p.ChatID) {
				continue
			}
			slog.Debug("ws: subagent_start",
				"span_id", p.SpanID,
				"parent_call_id", p.ParentSpawnCallID,
				"agent_id", p.AgentID,
			)
			sendConnFrame(wc, wsServerFrame{
				Type:         "subagent_start",
				SpanID:       p.SpanID,
				ParentCallID: string(p.ParentSpawnCallID),
				AgentID:      p.AgentID,
				TaskLabel:    p.TaskLabel,
			})
			// Register the span in openSpans for orphan watchdog tracking.
			entry := &openSpanEntry{
				spanID:       p.SpanID,
				parentCallID: string(p.ParentSpawnCallID),
				agentID:      p.AgentID,
				closeCh:      make(chan struct{}),
			}
			openSpans[string(p.ParentSpawnCallID)] = entry

		case agent.EventKindSubTurnEnd:
			// FR-H-004: emit subagent_end when a sub-turn finishes.
			p, ok := evt.Payload.(agent.SubTurnEndPayload)
			if !ok || !matchesChatID(p.ChatID) {
				continue
			}
			slog.Debug("ws: subagent_end",
				"span_id", p.SpanID,
				"parent_call_id", p.ParentSpawnCallID,
				"agent_id", p.AgentID,
			)
			sendConnFrame(wc, wsServerFrame{
				Type:         "subagent_end",
				SpanID:       p.SpanID,
				ParentCallID: string(p.ParentSpawnCallID),
				AgentID:      p.AgentID,
				Status:       string(p.Status),
				DurationMs:   p.DurationMS,
			})
			// Signal the watchdog that the span closed normally.
			closeSpan(string(p.ParentSpawnCallID))

		case agent.EventKindTurnEnd:
			// W1-2: only arm the orphan watchdog when the root turn for this
			// connection ends (IsRoot == true) and the event belongs to our chat
			// (ChatID matches). Sub-turn ends from sibling sub-turns would otherwise
			// spuriously interrupt still-running spans on this connection.
			p, ok := evt.Payload.(agent.TurnEndPayload)
			if !ok || !p.IsRoot || !matchesChatID(p.ChatID) {
				continue
			}
			// Determine watchdog reason from the terminal status of the parent turn.
			var watchdogReason string
			switch p.Status {
			case agent.TurnEndStatusAborted:
				watchdogReason = "parent_cancelled"
			case agent.TurnEndStatusError:
				watchdogReason = "parent_timeout"
			case agent.TurnEndStatusCompleted:
				watchdogReason = "parent_done_early"
			default:
				watchdogReason = "unknown"
			}
			for _, entry := range openSpans {
				if !entry.parentTurnEnded {
					entry.parentTurnEnded = true
					startOrphanWatchdog(entry, watchdogReason)
				}
			}

		case agent.EventKindToolExecStart:
			p, ok := evt.Payload.(agent.ToolExecStartPayload)
			if !ok || !matchesChatID(p.ChatID) {
				continue
			}
			// FR-H-005: propagate parent_call_id when the tool fires inside a sub-turn.
			// FR-I-008: propagate agent_id so live frames match replay frame parity.
			sendConnFrame(wc, wsServerFrame{
				Type:         "tool_call_start",
				CallID:       string(p.ToolCallID),
				Tool:         p.Tool,
				Params:       p.Arguments,
				ParentCallID: string(p.ParentSpawnCallID),
				AgentID:      p.AgentID,
			})
		case agent.EventKindToolExecEnd:
			p, ok := evt.Payload.(agent.ToolExecEndPayload)
			if !ok || !matchesChatID(p.ChatID) {
				continue
			}
			status := "success"
			if p.IsError {
				status = "error"
			}
			// FR-H-005: propagate parent_call_id when the tool fires inside a sub-turn.
			// FR-I-008: propagate agent_id so live frames match replay frame parity.
			sendConnFrame(wc, wsServerFrame{
				Type:         "tool_call_result",
				CallID:       string(p.ToolCallID),
				Tool:         p.Tool,
				Result:       p.Result,
				Status:       status,
				DurationMs:   p.Duration.Milliseconds(),
				ParentCallID: string(p.ParentSpawnCallID),
				AgentID:      p.AgentID,
			})
			// When the handoff tool succeeds, notify the frontend to switch agents.
			if p.Tool == "handoff" && status == "success" {
				if activeAgent, ok := h.agentLoop.GetSessionActiveAgent(chatID); ok {
					agentName, _ := h.agentLoop.GetRegistry().GetAgentName(activeAgent)
					sendConnFrame(wc, wsServerFrame{
						Type:    "agent_switched",
						AgentID: activeAgent,
						Message: agentName,
					})
				}
			}
			if p.Tool == "return_to_default" && status == "success" {
				defaultAgent := h.agentLoop.GetRegistry().GetDefaultAgent()
				var defaultName string
				if defaultAgent != nil {
					defaultName = defaultAgent.Name
				}
				sendConnFrame(wc, wsServerFrame{
					Type:    "agent_switched",
					AgentID: "", // empty = return to default
					Message: defaultName,
				})
			}
		case agent.EventKindRateLimit:
			// SEC-26: forward rate-limit denials to the browser so the chat UI
			// can display an inline indicator. Global-scope events (daily cost
			// cap) are broadcast to every connection since they are not tied
			// to a specific chatID.
			p, ok := evt.Payload.(agent.RateLimitPayload)
			if !ok {
				continue
			}
			if p.Scope != "global" && !matchesChatID(p.ChatID) {
				continue
			}
			sendConnFrame(wc, wsServerFrame{
				Type:              "rate_limit",
				Scope:             p.Scope,
				Resource:          p.Resource,
				PolicyRule:        p.PolicyRule,
				RetryAfterSeconds: p.RetryAfterSeconds,
				AgentID:           p.AgentID,
				Tool:              p.Tool,
			})
		}
	}
}

// wsStreamer implements bus.Streamer, pushing token/done frames into a wsConn's send channel.
// It also accumulates the full response to persist it to the session transcript on Finalize.
type wsStreamer struct {
	conn        *wsConn
	chatID      string
	sessionID   string                // for recording assistant message
	agentStore  *session.UnifiedStore // for recording assistant message
	agentID     string                // active agent at streamer creation time (for transcript AgentID)
	channel     *webchatChannel       // to mark streaming complete and suppress duplicate Send()
	accumulated strings.Builder       // accumulates full response text

	// Turn-level stats set by the agent loop via SetTurnStats before Finalize.
	// Populates the "done" frame so the chat UI shows real token counts and
	// cost instead of zeros (issue #12). Mutex-protected because SetTurnStats
	// and Finalize may be called from different goroutines.
	statsMu       sync.Mutex
	statsTokens   int64
	statsCostUSD  float64
	statsDuration time.Duration
}

// SetTurnStats is called by the agent loop's finalizeStreamer just before
// Finalize. Implements the streamerStatsSetter interface from pkg/agent.
func (s *wsStreamer) SetTurnStats(tokens int64, costUSD float64, duration time.Duration) {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	s.statsTokens = tokens
	s.statsCostUSD = costUSD
	s.statsDuration = duration
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
	if dropped := s.conn.droppedTokens.Load(); dropped > 0 {
		stats["tokens_dropped"] = dropped
	}
	// Include turn-level token/cost/duration if the agent loop pushed them via
	// SetTurnStats before this call (issue #12). Zero values are still emitted
	// so the client can reset the session counters for turns with no LLM usage.
	s.statsMu.Lock()
	stats["tokens"] = s.statsTokens
	stats["cost"] = s.statsCostUSD
	stats["duration_ms"] = s.statsDuration.Milliseconds()
	s.statsMu.Unlock()
	sendConnFrame(s.conn, wsServerFrame{Type: "done", Stats: stats})
	// Only mark as streamed if we actually sent content. If the LLM failed
	// before producing any tokens, let the outbound Send path deliver the
	// error message — otherwise the user sees a stuck "thinking" spinner.
	if s.channel != nil && s.accumulated.Len() > 0 {
		s.channel.markStreamed(s.chatID)
	}
	// Record the full assistant response to the session transcript.
	if s.agentStore != nil && s.sessionID != "" {
		content := s.accumulated.String()
		if content != "" {
			entry := session.TranscriptEntry{
				ID:        uuid.New().String(),
				Role:      "assistant",
				AgentID:   s.agentID,
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
