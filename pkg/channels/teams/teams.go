package teams

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/infracloudio/msbotbuilder-go/core"
	"github.com/infracloudio/msbotbuilder-go/core/activity"
	"github.com/infracloudio/msbotbuilder-go/schema"

	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/channels"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/identity"
	"github.com/dapicom-ai/omnipus/pkg/logger"
)

const (
	teamsMaxMessageLength  = 4000
	teamsWebhookPath        = "/api/messages"
	maxWebhookBodySize      = 1 << 20 // 1 MiB
)

// conversationRef stores the conversation reference for proactive messaging.
type conversationRef struct {
	ServiceURL     string
	ConversationID string
}

// TeamsChannel implements the channels.Channel interface for Microsoft Teams.
type TeamsChannel struct {
	*channels.BaseChannel
	config    config.TeamsConfig
	adapter   core.Adapter
	ctx       context.Context
	cancel    context.CancelFunc

	// chatType stores whether a chatID is a channel or direct message.
	// Channel IDs look like "19:abc@thread.tacv2" (contain "@thread.tacv2").
	// Direct message IDs are UPNs like "john@example.com" (contain no "@thread.").
	chatType sync.Map // chatID → "channel" | "direct"

	// convRefs stores conversation references for proactive messaging.
	// Keyed by chatID (conversation ID).
	convRefs sync.Map // chatID → conversationRef

	// lastMsgID stores the last message ID per chat for threading/replies.
	lastMsgID sync.Map // chatID → messageID
}

// NewTeamsChannel creates a new Teams channel.
func NewTeamsChannel(cfg config.TeamsConfig, messageBus *bus.MessageBus) (*TeamsChannel, error) {
	if cfg.AppID == "" || cfg.AppPassword.String() == "" {
		return nil, fmt.Errorf("teams app_id and app_password are required")
	}

	maxMsgLen := cfg.MaxMessageLength
	if maxMsgLen <= 0 {
		maxMsgLen = teamsMaxMessageLength
	}

	base := channels.NewBaseChannel("teams", cfg, messageBus, cfg.AllowFrom,
		channels.WithMaxMessageLength(maxMsgLen),
		channels.WithGroupTrigger(cfg.GroupTrigger),
		channels.WithReasoningChannelID(cfg.ReasoningChannelID),
	)

	return &TeamsChannel{
		BaseChannel: base,
		config:      cfg,
	}, nil
}

// Start initializes the Teams bot via msbotbuilder-go.
func (c *TeamsChannel) Start(ctx context.Context) error {
	logger.InfoC("teams", "Starting Microsoft Teams channel")

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Create the Bot Framework adapter with credentials.
	adapterSetting := core.AdapterSetting{
		AppID:       c.config.AppID,
		AppPassword: c.config.AppPassword.String(),
	}

	if c.config.TenantID != "" {
		adapterSetting.ChannelAuthTenant = c.config.TenantID
	}

	adapter, err := core.NewBotAdapter(adapterSetting)
	if err != nil {
		return fmt.Errorf("failed to create Teams adapter: %w", err)
	}
	c.adapter = adapter

	// Pre-register reasoning_channel_id as channel if configured,
	// so outbound-only destinations are routed correctly.
	if c.config.ReasoningChannelID != "" {
		c.chatType.Store(c.config.ReasoningChannelID, "channel")
	}

	c.SetRunning(true)
	logger.InfoC("teams", "Microsoft Teams channel started")
	return nil
}

// Stop gracefully stops the Teams channel.
func (c *TeamsChannel) Stop(ctx context.Context) error {
	logger.InfoC("teams", "Stopping Microsoft Teams channel")

	c.SetRunning(false)

	if c.cancel != nil {
		c.cancel()
	}

	return nil
}

// WebhookPath returns the path for registering on the shared HTTP server.
func (c *TeamsChannel) WebhookPath() string {
	return teamsWebhookPath
}

// ServeHTTP implements http.Handler for the shared HTTP server.
func (c *TeamsChannel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.webhookHandler(w, r)
}

// webhookHandler handles incoming Teams webhook requests.
func (c *TeamsChannel) webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodySize+1))
	if err != nil {
		logger.ErrorCF("teams", "Failed to read request body", map[string]any{
			"error": err.Error(),
		})
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > maxWebhookBodySize {
		logger.WarnC("teams", "Webhook request body too large, rejected")
		http.Error(w, "Request entity too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Parse the activity directly (auth is handled by gateway middleware).
	var act schema.Activity
	if err := json.Unmarshal(body, &act); err != nil {
		logger.ErrorCF("teams", "Failed to parse activity", map[string]any{
			"error": err.Error(),
		})
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Return 200 immediately to acknowledge receipt.
	// The activity is processed asynchronously via the message bus.
	w.WriteHeader(http.StatusOK)

	// Store conversation reference for proactive messaging.
	if act.Conversation.ID != "" {
		c.convRefs.Store(act.Conversation.ID, conversationRef{
			ServiceURL:     act.ServiceURL,
			ConversationID: act.Conversation.ID,
		})
	}

	// Process the activity asynchronously to avoid blocking the HTTP response.
	go c.processActivity(act)
}

// processActivity processes a Teams activity and routes it to the message bus.
func (c *TeamsChannel) processActivity(act schema.Activity) {
	// Use stored context if available, otherwise Background for unit tests.
	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Only handle message activities.
	if act.Type != schema.Message {
		logger.DebugCF("teams", "Ignoring non-message activity", map[string]any{
			"type": act.Type,
		})
		return
	}

	// Extract sender information.
	var senderID string
	if act.From.ID != "" {
		senderID = act.From.ID
	} else {
		logger.WarnC("teams", "Received message with no sender ID")
		return
	}

	// Extract content.
	content := strings.TrimSpace(act.Text)
	if content == "" && len(act.Attachments) == 0 {
		logger.DebugC("teams", "Received empty message with no attachments, ignoring")
		return
	}

	// Determine chat ID and type from conversation.
	var chatID string
	chatKind := c.getChatKindFromActivity(act)

	if act.Conversation.ID != "" {
		chatID = act.Conversation.ID
	}

	// Fallback to from.ID if no conversation ID.
	if chatID == "" {
		chatID = senderID
	}

	// Store the chat type for future routing.
	c.chatType.Store(chatID, chatKind)

	// Store last message ID for threading.
	if act.ID != "" {
		c.lastMsgID.Store(chatID, act.ID)
	}

	sender := bus.SenderInfo{
		Platform:    "teams",
		PlatformID:  senderID,
		CanonicalID: identity.BuildCanonicalID("teams", senderID),
	}

	// Check allowlist.
	if !c.IsAllowedSender(sender) {
		logger.DebugCF("teams", "Message rejected by allowlist", map[string]any{
			"sender_id": senderID,
		})
		return
	}

	// Determine peer kind.
	peerKind := bus.PeerGroup
	peerID := chatID
	if chatKind == "direct" {
		peerKind = bus.PeerDirect
		peerID = senderID
	}

	peer := bus.Peer{Kind: peerKind, ID: peerID}

	// Build metadata.
	metadata := map[string]string{
		"platform": "teams",
	}
	if act.ChannelData != nil {
		if tenant, ok := act.ChannelData["tenant"]; ok {
			if tenantStr, ok := tenant.(string); ok {
				metadata["tenant_id"] = tenantStr
			}
		}
	}

	logger.DebugCF("teams", "Received message", map[string]any{
		"sender_id": senderID,
		"chat_id":   chatID,
		"chat_kind": chatKind,
		"preview":   truncate(content, 50),
	})

	// Route to HandleMessage.
	c.HandleMessage(
		ctx,
		peer,
		act.ID,
		senderID,
		chatID,
		content,
		nil, // media
		metadata,
		sender,
	)

	// Send acknowledgment to Teams via the adapter's ProcessActivity.
	// This prevents Teams from retrying the message delivery.
	// We use a minimal handler that returns an empty activity.
	if c.adapter != nil {
		handler := activity.HandlerFuncs{
			OnMessageFunc: func(turn *activity.TurnContext) (schema.Activity, error) {
				// Return empty activity - we've already published to the bus.
				// This acknowledges the message without sending a duplicate response.
				return schema.Activity{}, nil
			},
		}
		_ = c.adapter.ProcessActivity(ctx, act, handler)
	}
}

// getChatKindFromActivity determines the chat type from the activity.
func (c *TeamsChannel) getChatKindFromActivity(act schema.Activity) string {
	// Check if this is a group/channel or direct message.
	// Teams channel IDs look like "19:abc@thread.tacv2" (contain "@thread.tacv2").
	// Direct message IDs are UPNs like "john@example.com" (no "@thread.").
	if act.Conversation.ID != "" {
		convID := act.Conversation.ID
		if strings.Contains(convID, "@thread.tacv2") {
			return "channel"
		}
		// Check if it's a group conversation.
		if act.Conversation.IsGroup {
			return "channel"
		}
		// If it contains @ but not @thread.tacv2, it's likely a UPN (direct).
		if strings.Contains(convID, "@") {
			return "direct"
		}
		// Default to channel for unknown format.
		return "channel"
	}
	return "channel"
}

// Send sends an outbound message to Teams using proactive messaging.
func (c *TeamsChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	chatKind := c.getChatKind(msg.ChatID)

	// Look up the conversation reference for this chat.
	refVal, ok := c.convRefs.Load(msg.ChatID)
	if !ok {
		logger.WarnCF("teams", "No conversation reference found for chatID, cannot send", map[string]any{
			"chat_id": msg.ChatID,
		})
		return fmt.Errorf("teams: no conversation reference for chatID %s", msg.ChatID)
	}
	ref := refVal.(conversationRef)

	// Build the proactive message conversation reference.
	convRef := schema.ConversationReference{
		ServiceURL: ref.ServiceURL,
		Conversation: schema.ConversationAccount{
			ID: ref.ConversationID,
		},
	}

	// Create a handler that returns the message as an activity.
	handler := activity.HandlerFuncs{
		OnMessageFunc: func(turn *activity.TurnContext) (schema.Activity, error) {
			activity := schema.Activity{
				Type: schema.Message,
				Text: msg.Content,
			}
			return activity, nil
		},
	}

	// Send via proactive messaging.
	if err := c.adapter.ProactiveMessage(ctx, convRef, handler); err != nil {
		logger.ErrorCF("teams", "Failed to send message", map[string]any{
			"chat_id":   msg.ChatID,
			"chat_kind": chatKind,
			"error":     err.Error(),
		})
		return fmt.Errorf("teams send: %w", channels.ErrTemporary)
	}

	logger.DebugCF("teams", "Message sent", map[string]any{
		"chat_id":   msg.ChatID,
		"chat_kind": chatKind,
	})

	return nil
}

// getChatKind returns the chat type for a given chatID ("channel" or "direct").
// Channel IDs contain "@thread.tacv2", direct message IDs are UPNs (contain "@" but not "@thread.tacv2").
// When no cached value exists, it detects the type directly from the chatID format.
func (c *TeamsChannel) getChatKind(chatID string) string {
	if v, ok := c.chatType.Load(chatID); ok {
		if k, ok := v.(string); ok {
			return k
		}
	}
	// Detect from chatID format when not cached.
	// Channel IDs look like "19:abc@thread.tacv2".
	// Direct message IDs are UPNs (contain "@" but not "@thread.tacv2").
	if strings.Contains(chatID, "@thread.tacv2") {
		return "channel"
	}
	if strings.Contains(chatID, "@") {
		return "direct"
	}
	// Default to "channel" for unknown chat IDs.
	return "channel"
}

// truncate truncates a string to a maximum length.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "..."
}
