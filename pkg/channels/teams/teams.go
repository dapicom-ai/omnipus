package teams

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/infracloudio/msbotbuilder-go/core"
	"github.com/infracloudio/msbotbuilder-go/core/activity"
	"github.com/infracloudio/msbotbuilder-go/schema"

	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/channels"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/credentials"
	"github.com/dapicom-ai/omnipus/pkg/identity"
	"github.com/dapicom-ai/omnipus/pkg/logger"
)

const (
	teamsMaxMessageLength = 4000
	teamsWebhookPath      = "/api/messages"
	maxWebhookBodySize    = 1 << 20 // 1 MiB
)

// Teams API endpoints use these domains. ServiceURL validation prevents SSRF.
var allowedServiceURLPrefixes = []string{
	"https://teams.microsoft.com",
	"https://smba.trafficmanager.net",
	"https://teams.cloud.gov",
}

// conversationRef stores the conversation reference for proactive messaging.
type conversationRef struct {
	ServiceURL     string
	ConversationID string
}

const (
	chatKindChannel = "channel"
	chatKindDirect  = "direct"
)

// TeamsChannel implements the channels.Channel interface for Microsoft Teams.
type TeamsChannel struct {
	*channels.BaseChannel
	config     config.TeamsConfig
	appPassword string // plaintext password, not stored after construction
	adapter    core.Adapter
	ctx        context.Context
	cancel     context.CancelFunc
	stopOnce   sync.Once

	// activeGoroutines tracks in-flight processActivity goroutines for graceful shutdown.
	activeGoroutines sync.WaitGroup

	// chatType stores whether a chatID is a channel or direct message.
	// Channel IDs look like "19:abc@thread.tacv2" (contain "@thread.tacv2").
	// Direct message IDs are UPNs like "john@example.com" (contain no "@thread.").
	chatType sync.Map // chatID → chatKindChannel | chatKindDirect

	// convRefs stores conversation references for proactive messaging.
	// Keyed by chatID (conversation ID).
	convRefs sync.Map // chatID → conversationRef

	// lastMsgID stores the last message ID per chat for threading/replies.
	lastMsgID sync.Map // chatID → messageID
}

// NewTeamsChannel creates a new Teams channel with the given configuration.
// Returns nil if AppID or AppPassword is empty. MaxMessageLength defaults to 4000
// if not specified or set to a non-positive value.
func NewTeamsChannel(cfg config.TeamsConfig, secrets credentials.SecretBundle, messageBus *bus.MessageBus) (*TeamsChannel, error) {
	appPassword := secrets.GetString(cfg.AppPasswordRef)
	if cfg.AppID == "" || appPassword == "" {
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
		BaseChannel:  base,
		config:      cfg,
		appPassword: appPassword,
	}, nil
}

// isValidServiceURL returns true if the URL has an allowed prefix.
// This prevents SSRF attacks where a malicious webhook could store an internal
// network URL (e.g., http://169.254.169.254/) as the ServiceURL.
func isValidServiceURL(serviceURL string) bool {
	for _, prefix := range allowedServiceURLPrefixes {
		if strings.HasPrefix(serviceURL, prefix) {
			return true
		}
	}
	return false
}

// Start initializes the Teams bot via msbotbuilder-go and sets running=true.
// Returns an error if adapter creation fails. Pre-registers ReasoningChannelID as "channel"
// in the chatType map if configured.
func (c *TeamsChannel) Start(ctx context.Context) error {
	logger.InfoC("teams", "Starting Microsoft Teams channel")

	c.ctx, c.cancel = context.WithCancel(ctx)

	adapterSetting := core.AdapterSetting{
		AppID:       c.config.AppID,
		AppPassword: c.appPassword,
	}

	if c.config.TenantID != "" {
		adapterSetting.ChannelAuthTenant = c.config.TenantID
	}

	adapter, err := core.NewBotAdapter(adapterSetting)
	if err != nil {
		return fmt.Errorf("failed to create Teams adapter: %w", err)
	}
	c.adapter = adapter

	if c.config.ReasoningChannelID != "" {
		c.chatType.Store(c.config.ReasoningChannelID, chatKindChannel)
	}

	c.SetRunning(true)
	logger.InfoC("teams", "Microsoft Teams channel started")
	return nil
}

// Stop gracefully stops the Teams channel. Sets running=false, cancels the context,
// and waits up to 10 seconds for all in-flight processActivity goroutines to complete.
func (c *TeamsChannel) Stop(ctx context.Context) error {
	logger.InfoC("teams", "Stopping Microsoft Teams channel")

	c.SetRunning(false)

	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		// Wait for all in-flight processActivity goroutines to complete.
		// Use a timeout to avoid indefinite blocking if a goroutine hangs.
		done := make(chan struct{})
		go func() {
			c.activeGoroutines.Wait()
			close(done)
		}()
		select {
		case <-done:
			// All goroutines completed normally
		case <-time.After(10 * time.Second):
			logger.WarnC("teams", "Stop() timed out waiting for goroutines to complete")
		}
	})

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

	// Store validated conversation reference for proactive messaging.
	// SSRF guard: reject ServiceURLs that don't point to known Teams endpoints.
	if act.Conversation.ID != "" && act.ServiceURL != "" && isValidServiceURL(act.ServiceURL) {
		c.convRefs.Store(act.Conversation.ID, conversationRef{
			ServiceURL:     act.ServiceURL,
			ConversationID: act.Conversation.ID,
		})
	}

	// Process the activity asynchronously to avoid blocking the HTTP response.
	// Register with WaitGroup so Stop() can wait for completion.
	c.activeGoroutines.Add(1)
	go c.processActivity(act)
}

// processActivity processes a Teams activity and routes it to the message bus.
func (c *TeamsChannel) processActivity(act schema.Activity) {
	defer c.activeGoroutines.Done()

	// Panic recovery prevents goroutine leaks from unexpected panics in HandleMessage.
	defer func() {
		if r := recover(); r != nil {
			logger.ErrorCF("teams", "Panic in processActivity goroutine", map[string]any{
				"panic":       r,
				"activity_id": act.ID,
			})
		}
	}()

	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if act.Type != schema.Message {
		logger.DebugCF("teams", "Ignoring non-message activity", map[string]any{
			"type": act.Type,
		})
		return
	}

	var senderID string
	if act.From.ID != "" {
		senderID = act.From.ID
	} else {
		logger.WarnC("teams", "Received message with no sender ID")
		return
	}

	content := strings.TrimSpace(act.Text)
	if content == "" && len(act.Attachments) == 0 {
		logger.WarnCF("teams", "Received empty message with no attachments, ignoring", map[string]any{
			"activity_id": act.ID,
		})
		return
	}

	chatID := act.Conversation.ID
	if chatID == "" {
		chatID = senderID
	}

	chatKind := c.detectChatKind(act.Conversation.ID, act.Conversation.IsGroup)
	c.chatType.Store(chatID, chatKind)

	if act.ID != "" {
		c.lastMsgID.Store(chatID, act.ID)
	}

	sender := bus.SenderInfo{
		Platform:    "teams",
		PlatformID:  senderID,
		CanonicalID: identity.BuildCanonicalID("teams", senderID),
	}

	if !c.IsAllowedSender(sender) {
		logger.WarnCF("teams", "Message rejected by allowlist", map[string]any{
			"sender_id": senderID,
			"chat_id":   chatID,
		})
		return
	}

	peerKind := bus.PeerGroup
	peerID := chatID
	if chatKind == chatKindDirect {
		peerKind = bus.PeerDirect
		peerID = senderID
	}

	peer := bus.Peer{Kind: peerKind, ID: peerID}

	metadata := map[string]string{
		"platform": "teams",
	}
	if tenantID := extractTenantID(act.ChannelData); tenantID != "" {
		metadata["tenant_id"] = tenantID
	}

	logger.DebugCF("teams", "Received message", map[string]any{
		"sender_id": senderID,
		"chat_id":   chatID,
		"chat_kind": chatKind,
		"preview":   truncate(content, 50),
	})

	c.HandleMessage(
		ctx,
		peer,
		act.ID,
		senderID,
		chatID,
		content,
		nil,
		metadata,
		sender,
	)

	// Send acknowledgment to Teams to prevent retry storms.
	// Log errors but do not return — message was already processed.
	if c.adapter != nil {
		handler := activity.HandlerFuncs{
			OnMessageFunc: func(turn *activity.TurnContext) (schema.Activity, error) {
				return schema.Activity{}, nil
			},
		}
		if err := c.adapter.ProcessActivity(ctx, act, handler); err != nil {
			logger.ErrorCF("teams", "Failed to acknowledge activity to Teams (may cause retry)", map[string]any{
				"activity_id": act.ID,
				"error":      err.Error(),
			})
		}
	}
}

// detectChatKindFromID determines chat kind from a conversation ID string.
func detectChatKindFromID(convID string) string {
	if strings.Contains(convID, "@thread.tacv2") {
		return chatKindChannel
	}
	if strings.Contains(convID, "@") {
		return chatKindDirect
	}
	return chatKindChannel
}

// detectChatKind determines chat kind from conversation ID and group flag.
func (c *TeamsChannel) detectChatKind(convID string, isGroup bool) string {
	if convID == "" {
		return chatKindChannel
	}
	if isGroup {
		return chatKindChannel
	}
	return detectChatKindFromID(convID)
}

// Send sends an outbound message to Teams using proactive messaging.
func (c *TeamsChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	chatKind := c.getChatKind(msg.ChatID)

	refVal, ok := c.convRefs.Load(msg.ChatID)
	if !ok {
		logger.WarnCF("teams", "No conversation reference found for chatID, cannot send", map[string]any{
			"chat_id": msg.ChatID,
		})
		return fmt.Errorf("teams: no conversation reference for chatID %s: %w", msg.ChatID, channels.ErrTemporary)
	}
	ref, ok := refVal.(conversationRef)
	if !ok {
		logger.ErrorCF("teams", "Corrupted conversation reference for chatID", map[string]any{
			"chat_id": msg.ChatID,
			"type":    fmt.Sprintf("%T", refVal),
		})
		return fmt.Errorf("teams: corrupted conversation reference for chatID %s: %w", msg.ChatID, channels.ErrTemporary)
	}

	convRef := schema.ConversationReference{
		ServiceURL: ref.ServiceURL,
		Conversation: schema.ConversationAccount{
			ID: ref.ConversationID,
		},
	}

	handler := activity.HandlerFuncs{
		OnMessageFunc: func(turn *activity.TurnContext) (schema.Activity, error) {
			return schema.Activity{
				Type: schema.Message,
				Text: msg.Content,
			}, nil
		},
	}

	if c.adapter == nil {
		logger.ErrorCF("teams", "Teams adapter is nil, cannot send", map[string]any{
			"chat_id": msg.ChatID,
		})
		return fmt.Errorf("teams: adapter not initialized: %w", channels.ErrTemporary)
	}

	if err := c.adapter.ProactiveMessage(ctx, convRef, handler); err != nil {
		logger.ErrorCF("teams", "Failed to send proactive message to Teams", map[string]any{
			"chat_id":   msg.ChatID,
			"chat_kind": chatKind,
			"error":     err.Error(),
		})
		return fmt.Errorf("teams send failed for chatID %s: %w", msg.ChatID, channels.ErrTemporary)
	}

	logger.DebugCF("teams", "Message sent", map[string]any{
		"chat_id":   msg.ChatID,
		"chat_kind": chatKind,
	})

	return nil
}

// getChatKind returns the chat type for a given chatID (cached or detected).
func (c *TeamsChannel) getChatKind(chatID string) string {
	if v, ok := c.chatType.Load(chatID); ok {
		if k, ok := v.(string); ok {
			return k
		}
		logger.WarnCF("teams", "Corrupted chat type entry for chatID, falling back to detection", map[string]any{
			"chat_id": chatID,
			"type":    fmt.Sprintf("%T", v),
		})
	}
	return detectChatKindFromID(chatID)
}

// extractTenantID extracts the tenant ID from channel data if present.
func extractTenantID(channelData map[string]any) string {
	if channelData == nil {
		return ""
	}
	tenant, ok := channelData["tenant"]
	if !ok {
		return ""
	}
	tenantStr, ok := tenant.(string)
	if !ok {
		return ""
	}
	return tenantStr
}

// truncate truncates a string to a maximum rune count, appending "..." if truncation occurred.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return "..."
	}
	if len(s) <= maxLen {
		return s
	}
	return string([]rune(s)[:maxLen]) + "..."
}
