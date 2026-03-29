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
	"net/http"
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
	Type      string `json:"type"`               // "auth" | "message" | "cancel" | "exec_approval_response"
	Token     string `json:"token,omitempty"`    // for "auth"
	Content   string `json:"content,omitempty"` // for "message"
	SessionID string `json:"session_id,omitempty"` // for "cancel"
	ID        string `json:"id,omitempty"`       // for "exec_approval_response"
	Decision  string `json:"decision,omitempty"` // "allow" | "deny" | "always"
}

// wsServerFrame is a message sent from the server to the browser over WebSocket.
type wsServerFrame struct {
	Type    string         `json:"type"`
	Content string         `json:"content,omitempty"`
	Tool    string         `json:"tool,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
	Command string         `json:"command,omitempty"`
	ID      string         `json:"id,omitempty"`
	Stats   map[string]any `json:"stats,omitempty"`
	Message string         `json:"message,omitempty"`
}

// WSHandler handles the /api/v1/chat/ws WebSocket endpoint for bi-directional
// chat streaming. It implements bus.StreamDelegate so the agent loop can push
// tokens directly to the connected browser. Replaces the Wave 1 SSE handler
// per Wave 5a spec (non-behavior: must not use SSE for chat streaming).
type WSHandler struct {
	msgBus        *bus.MessageBus
	agentLoop     *agent.AgentLoop
	partitions    *session.PartitionStore // may be nil
	allowedOrigin string

	mu       sync.Mutex
	sessions map[string]*wsConn // chatID → connection

	upgrader websocket.Upgrader
}

type wsConn struct {
	conn      *websocket.Conn
	sendCh    chan []byte
	doneCh    chan struct{}
	closeOnce sync.Once
}

func (c *wsConn) close() {
	c.closeOnce.Do(func() { close(c.doneCh) })
}

// newWSHandler creates a WSHandler and registers it as the MessageBus stream delegate,
// replacing any previously registered delegate (e.g., the Wave 1 SSE handler).
func newWSHandler(
	msgBus *bus.MessageBus,
	agentLoop *agent.AgentLoop,
	ps *session.PartitionStore,
	allowedOrigin string,
) *WSHandler {
	h := &WSHandler{
		msgBus:        msgBus,
		agentLoop:     agentLoop,
		partitions:    ps,
		allowedOrigin: allowedOrigin,
		sessions:      make(map[string]*wsConn),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true // non-browser or same-origin
				}
				if allowedOrigin != "" && origin == allowedOrigin {
					return true
				}
				// Always allow localhost origins for development.
				return strings.HasPrefix(origin, "http://localhost") ||
					strings.HasPrefix(origin, "http://127.0.0.1")
			},
		},
	}
	msgBus.SetStreamDelegate(h)
	return h
}

// GetStreamer implements bus.StreamDelegate.
// Returns a WebSocket streamer for webchat sessions that have an active connection.
func (h *WSHandler) GetStreamer(_ context.Context, channel, chatID string) (bus.Streamer, bool) {
	if channel != "webchat" {
		return nil, false
	}
	h.mu.Lock()
	conn, ok := h.sessions[chatID]
	h.mu.Unlock()
	if !ok {
		return nil, false
	}
	return &wsStreamer{conn: conn}, true
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
	defer func() {
		h.mu.Lock()
		delete(h.sessions, chatID)
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
			return
		}

		var frame wsClientFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			slog.Warn("ws: malformed frame", "error", err)
			continue
		}

		switch frame.Type {
		case "message":
			if frame.Content == "" {
				continue
			}
			h.handleChatMessage(ctx, chatID, sessionID, frame.Content, wc)
		case "cancel":
			h.handleCancel(sessionID)
		case "exec_approval_response":
			slog.Debug("ws: exec_approval_response received (not yet wired)", "id", frame.ID, "decision", frame.Decision)
		}
	}
}

// handleChatMessage creates a session on the first message, records every user message,
// and publishes the message to the bus. If session creation fails, the client is warned
// that the conversation will not be persisted (fix for silent persistence failure).
func (h *WSHandler) handleChatMessage(ctx context.Context, chatID string, sessionID *string, content string, wc *wsConn) {
	if h.partitions != nil {
		// Create the session on the first message of this WebSocket connection.
		if *sessionID == "" {
			meta, err := h.partitions.NewSession("webchat", "", "")
			if err != nil {
				slog.Warn("ws: could not create session — conversation will not be saved", "error", err)
				sendConnFrame(wc, wsServerFrame{
					Type:    "error",
					Message: "warning: could not create session — conversation will not be saved",
				})
				// Continue without sessionID so the message is still delivered to the agent.
			} else {
				*sessionID = meta.ID
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
			if err := h.partitions.AppendMessage(*sessionID, entry); err != nil {
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
	if h.partitions != nil && sessionID != nil && *sessionID != "" {
		if err := h.partitions.SetStatus(*sessionID, "interrupted"); err != nil {
			slog.Warn("ws: could not mark session interrupted", "session_id", *sessionID, "error", err)
		}
	}
}

// wsPingMsg is a nil sentinel enqueued by pingPump to signal writePump to send a WebSocket ping.
// Using a sentinel through sendCh ensures all writes go through the single writer goroutine,
// satisfying gorilla/websocket's single-writer requirement (fix for gorilla write race).
var wsPingMsg []byte = nil

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
	case "done", "error":
		// Critical frames must not be dropped. Block briefly; force-close on timeout.
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

// wsStreamer implements bus.Streamer, pushing token/done frames into a wsConn's send channel.
type wsStreamer struct {
	conn *wsConn
}

func (s *wsStreamer) Update(_ context.Context, content string) error {
	sendConnFrame(s.conn, wsServerFrame{Type: "token", Content: content})
	return nil
}

func (s *wsStreamer) Finalize(_ context.Context, _ string) error {
	sendConnFrame(s.conn, wsServerFrame{Type: "done", Stats: map[string]any{}})
	return nil
}

func (s *wsStreamer) Cancel(_ context.Context) {
	s.conn.close()
}
