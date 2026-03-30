// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/dapicom-ai/omnipus/pkg/bus"
)

// webchatChannel implements channels.Channel so the channel Manager can route
// outbound messages back to WebSocket clients. Without this, the Manager's
// dispatch loop logs "Unknown channel for outbound message" and drops responses.
//
// Streaming tokens are delivered separately via the WSHandler's GetStreamer path
// (registered as the Manager's streamFallback). This channel handles the final
// assembled outbound message that the agent loop publishes via PublishOutbound.
type webchatChannel struct {
	wsHandler *WSHandler
}

func (c *webchatChannel) Name() string { return "webchat" }

func (c *webchatChannel) Start(_ context.Context) error { return nil }
func (c *webchatChannel) Stop(_ context.Context) error  { return nil }
func (c *webchatChannel) IsRunning() bool                { return true }
func (c *webchatChannel) IsAllowed(_ string) bool        { return true }
func (c *webchatChannel) IsAllowedSender(_ bus.SenderInfo) bool { return true }
func (c *webchatChannel) ReasoningChannelID() string     { return "" }

// Send delivers an outbound message to the WebSocket client identified by ChatID.
// The message is sent as a "message" frame (complete response, not streaming token).
func (c *webchatChannel) Send(_ context.Context, msg bus.OutboundMessage) error {
	c.wsHandler.mu.Lock()
	conn, ok := c.wsHandler.sessions[msg.ChatID]
	c.wsHandler.mu.Unlock()

	if !ok {
		slog.Debug("webchat: no active connection for outbound message", "chat_id", msg.ChatID)
		return nil // client disconnected — not an error
	}

	frame := wsServerFrame{
		Type:    "message",
		Content: msg.Content,
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}

	select {
	case conn.sendCh <- data:
	default:
		slog.Warn("webchat: send channel full, outbound message dropped", "chat_id", msg.ChatID)
		// TODO(webchat-media): add support for media/attachment messages once the
		// OutboundMessage type includes media fields.
		return fmt.Errorf("webchat: send channel full for chat %s", msg.ChatID)
	}
	return nil
}
