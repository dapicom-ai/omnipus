//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/dapicom-ai/omnipus/pkg/bus"
)

// webchatChannel implements channels.Channel so the channel Manager can route
// outbound messages back to WebSocket clients. Without this, the Manager's
// dispatch loop logs "Unknown channel for outbound message" and drops responses.
//
// When streaming is active (via WSHandler's GetStreamer → wsStreamer), tokens
// are delivered incrementally and Finalize sends the "done" frame. In that case,
// Send() is a no-op to avoid duplicating the response.
type webchatChannel struct {
	wsHandler *WSHandler

	// streamed tracks chatIDs where wsStreamer.Finalize has already delivered
	// the response. Send() skips these to avoid duplication.
	mu       sync.Mutex
	streamed map[string]bool
}

func newWebchatChannel(wsHandler *WSHandler) *webchatChannel {
	return &webchatChannel{
		wsHandler: wsHandler,
		streamed:  make(map[string]bool),
	}
}

func (c *webchatChannel) markStreamed(chatID string) {
	c.mu.Lock()
	c.streamed[chatID] = true
	c.mu.Unlock()
}

func (c *webchatChannel) Name() string { return "webchat" }

func (c *webchatChannel) Start(_ context.Context) error         { return nil }
func (c *webchatChannel) Stop(_ context.Context) error          { return nil }
func (c *webchatChannel) IsRunning() bool                       { return true }
func (c *webchatChannel) IsAllowed(_ string) bool               { return true }
func (c *webchatChannel) IsAllowedSender(_ bus.SenderInfo) bool { return true }
func (c *webchatChannel) ReasoningChannelID() string            { return "" }

// Send delivers an outbound message to the WebSocket client.
// If the response was already delivered via streaming (wsStreamer), this is a no-op.
func (c *webchatChannel) Send(_ context.Context, msg bus.OutboundMessage) error {
	c.mu.Lock()
	alreadyStreamed := c.streamed[msg.ChatID]
	delete(c.streamed, msg.ChatID) // consume the flag
	c.mu.Unlock()

	if alreadyStreamed {
		slog.Debug("webchat: skipping Send — response already delivered via streaming", "chat_id", msg.ChatID)
		return nil
	}

	c.wsHandler.mu.Lock()
	conn, ok := c.wsHandler.sessions[msg.ChatID]
	c.wsHandler.mu.Unlock()

	if !ok {
		return fmt.Errorf("webchat: no active connection for chat %s", msg.ChatID)
	}

	if msg.Content != "" {
		sendConnFrame(conn, wsServerFrame{Type: "token", Content: msg.Content})
	}
	sendConnFrame(conn, wsServerFrame{Type: "done", Stats: map[string]any{}})
	return nil
}

// SendMedia delivers media attachments to the WebSocket client.
// Implements channels.MediaSender so the channel manager can route
// OutboundMediaMessage to the webchat channel.
func (c *webchatChannel) SendMedia(_ context.Context, msg bus.OutboundMediaMessage) error {
	if len(msg.Parts) == 0 {
		slog.Warn("webchat: SendMedia called with empty parts — skipping", "chat_id", msg.ChatID)
		return nil
	}

	c.wsHandler.mu.Lock()
	conn, ok := c.wsHandler.sessions[msg.ChatID]
	c.wsHandler.mu.Unlock()

	if !ok {
		return fmt.Errorf("webchat: no active connection for chat %s", msg.ChatID)
	}

	parts := make([]wsMediaPart, 0, len(msg.Parts))
	for _, p := range msg.Parts {
		if !strings.HasPrefix(p.Ref, "media://") {
			slog.Warn("webchat: media part has unexpected ref scheme — skipping",
				"chat_id", msg.ChatID, "ref", p.Ref)
			continue
		}
		parts = append(parts, wsMediaPart{
			Type:        p.Type,
			URL:         "/api/v1/media/" + strings.TrimPrefix(p.Ref, "media://"),
			Filename:    p.Filename,
			ContentType: p.ContentType,
			Caption:     p.Caption,
		})
	}
	if len(parts) == 0 {
		slog.Warn("webchat: all media parts skipped — not sending frame", "chat_id", msg.ChatID)
		return nil
	}

	slog.Debug("webchat: sending media frame", "chat_id", msg.ChatID, "parts", len(parts))
	sendConnFrame(conn, wsServerFrame{Type: "media", Parts: parts})
	return nil
}
