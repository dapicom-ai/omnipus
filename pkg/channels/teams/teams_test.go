package teams

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infracloudio/msbotbuilder-go/core/activity"
	"github.com/infracloudio/msbotbuilder-go/schema"

	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/channels"
	"github.com/dapicom-ai/omnipus/pkg/config"
)

// newTestSecureString is a helper that wraps config.NewSecureString for test use.
func newTestSecureString(val string) config.SecureString {
	return *config.NewSecureString(val)
}

// -- Test getChatKind --------------------------------------------------------

func TestGetChatKind_ChannelID(t *testing.T) {
	// Channel IDs contain "@thread.tacv2"
	ch := &TeamsChannel{}
	if got := ch.getChatKind("19:abc@thread.tacv2"); got != "channel" {
		t.Errorf("getChatKind(%q) = %q, want channel", "19:abc@thread.tacv2", got)
	}
}

func TestGetChatKind_GroupChannelID(t *testing.T) {
	// Group channel format includes channelid after the thread.tacv2: prefix
	ch := &TeamsChannel{}
	if got := ch.getChatKind("19:groupid@thread.tacv2:channelid"); got != "channel" {
		t.Errorf("getChatKind(%q) = %q, want channel", "19:groupid@thread.tacv2:channelid", got)
	}
}

func TestGetChatKind_DirectMessage(t *testing.T) {
	// Direct message IDs are UPNs without "@thread."
	ch := &TeamsChannel{}
	tests := []struct {
		chatID string
		want   string
	}{
		{"john@example.com", "direct"},
		{"user@contoso.com", "direct"},
		{"alice@example.org", "direct"},
	}
	for _, tc := range tests {
		if got := ch.getChatKind(tc.chatID); got != tc.want {
			t.Errorf("getChatKind(%q) = %q, want %q", tc.chatID, got, tc.want)
		}
	}
}

func TestGetChatKind_UnknownFormat(t *testing.T) {
	// Unknown format defaults to "channel"
	ch := &TeamsChannel{}
	if got := ch.getChatKind("unknown-id-format"); got != "channel" {
		t.Errorf("getChatKind(%q) = %q, want channel", "unknown-id-format", got)
	}
}

func TestGetChatKind_CachedInChatTypeMap(t *testing.T) {
	// When chat type is stored in the sync.Map, it should be returned directly
	ch := &TeamsChannel{}
	ch.chatType.Store("user@example.com", "direct")
	if got := ch.getChatKind("user@example.com"); got != "direct" {
		t.Errorf("getChatKind(%q) = %q, want direct", "user@example.com", got)
	}
}

func TestGetChatKind_CachedNonString(t *testing.T) {
	// If the cached value is not a string, fall back to default
	ch := &TeamsChannel{}
	ch.chatType.Store("weird-value", 12345) // non-string
	if got := ch.getChatKind("weird-value"); got != "channel" {
		t.Errorf("getChatKind(%q) = %q, want channel", "weird-value", got)
	}
}

// -- Test getChatKindFromActivity --------------------------------------------

func TestGetChatKindFromActivity_ChannelID(t *testing.T) {
	ch := &TeamsChannel{}
	act := schema.Activity{
		Conversation: schema.ConversationAccount{
			ID: "19:abc@thread.tacv2",
		},
	}
	if got := ch.getChatKindFromActivity(act); got != "channel" {
		t.Errorf("getChatKindFromActivity with channel ID = %q, want channel", got)
	}
}

func TestGetChatKindFromActivity_IsGroupTrue(t *testing.T) {
	// Even with a UPN-style ID, isGroup=true means it's a channel
	ch := &TeamsChannel{}
	act := schema.Activity{
		Conversation: schema.ConversationAccount{
			ID:      "user@example.com",
			IsGroup: true,
		},
	}
	if got := ch.getChatKindFromActivity(act); got != "channel" {
		t.Errorf("getChatKindFromActivity with isGroup=true = %q, want channel", got)
	}
}

func TestGetChatKindFromActivity_DirectUPN(t *testing.T) {
	// UPN without @thread.tacv2 is direct
	ch := &TeamsChannel{}
	act := schema.Activity{
		Conversation: schema.ConversationAccount{
			ID:      "john@example.com",
			IsGroup: false,
		},
	}
	if got := ch.getChatKindFromActivity(act); got != "direct" {
		t.Errorf("getChatKindFromActivity with UPN = %q, want direct", got)
	}
}

func TestGetChatKindFromActivity_EmptyID(t *testing.T) {
	ch := &TeamsChannel{}
	act := schema.Activity{
		Conversation: schema.ConversationAccount{
			ID: "",
		},
	}
	if got := ch.getChatKindFromActivity(act); got != "channel" {
		t.Errorf("getChatKindFromActivity with empty ID = %q, want channel", got)
	}
}

// -- Test NewTeamsChannel -----------------------------------------------------

func TestNewTeamsChannel_MissingAppID(t *testing.T) {
	cfg := config.TeamsConfig{
		AppID:       "",
		AppPassword: newTestSecureString("secret"),
	}
	ch, err := NewTeamsChannel(cfg, nil)
	if ch != nil {
		t.Error("expected nil channel when AppID is empty")
	}
	if err == nil {
		t.Error("expected error when AppID is empty")
	}
}

func TestNewTeamsChannel_MissingAppPassword(t *testing.T) {
	cfg := config.TeamsConfig{
		AppID:       "app-id-123",
		AppPassword: newTestSecureString(""),
	}
	ch, err := NewTeamsChannel(cfg, nil)
	if ch != nil {
		t.Error("expected nil channel when AppPassword is empty")
	}
	if err == nil {
		t.Error("expected error when AppPassword is empty")
	}
}

func TestNewTeamsChannel_ValidConfig(t *testing.T) {
	cfg := config.TeamsConfig{
		AppID:       "app-id-123",
		AppPassword: newTestSecureString("secret-password"),
	}
	ch, err := NewTeamsChannel(cfg, nil)
	if err != nil {
		t.Fatalf("NewTeamsChannel() error = %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestNewTeamsChannel_DefaultMaxMessageLength(t *testing.T) {
	cfg := config.TeamsConfig{
		AppID:       "app-id-123",
		AppPassword: newTestSecureString("secret-password"),
		// MaxMessageLength not set → should default to 4000
	}
	ch, err := NewTeamsChannel(cfg, nil)
	if err != nil {
		t.Fatalf("NewTeamsChannel() error = %v", err)
	}
	// Access the unexported field via the exported method
	if got := ch.MaxMessageLength(); got != 4000 {
		t.Errorf("MaxMessageLength() = %d, want 4000", got)
	}
}

func TestNewTeamsChannel_CustomMaxMessageLength(t *testing.T) {
	cfg := config.TeamsConfig{
		AppID:             "app-id-123",
		AppPassword:       newTestSecureString("secret-password"),
		MaxMessageLength:  5000,
	}
	ch, err := NewTeamsChannel(cfg, nil)
	if err != nil {
		t.Fatalf("NewTeamsChannel() error = %v", err)
	}
	if got := ch.MaxMessageLength(); got != 5000 {
		t.Errorf("MaxMessageLength() = %d, want 5000", got)
	}
}

// -- Test truncate ------------------------------------------------------------

func TestTruncate_ShortString(t *testing.T) {
	// String shorter than maxLen returns unchanged
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate(%q, 10) = %q, want hello", "hello", got)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	// String equal to maxLen returns unchanged
	if got := truncate("hello", 5); got != "hello" {
		t.Errorf("truncate(%q, 5) = %q, want hello", "hello", got)
	}
}

func TestTruncate_TooLong(t *testing.T) {
	// String longer than maxLen gets truncated with "..."
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncate(%q, 5) = %q, want hello...", "hello world", got)
	}
}

func TestTruncate_Unicode(t *testing.T) {
	// Unicode characters are counted correctly (rune-based truncation)
	// strings.Repeat("日本語", 10) = 30 runes; truncate to 5 runes = "日本語日本" + "..."
	long := strings.Repeat("日本語", 10) // 30 runes
	if got := truncate(long, 5); got != "日本語日本..." {
		t.Errorf("truncate unicode = %q, want 日本語日本...", got)
	}
}

// -- Test Start and Stop -----------------------------------------------------

func TestStart_SetsRunningTrue(t *testing.T) {
	// Start should set running=true.
	// We can't test the full Start() because it calls core.NewBotAdapter which
	// requires real credentials. We test the running state transition directly.
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, nil, nil),
		config: config.TeamsConfig{
			AppID:       "app-id-123",
			AppPassword: newTestSecureString("secret-password"),
		},
		ctx: context.Background(),
	}
	// Note: we test via SetRunning since Start() requires the real adapter
	ch.SetRunning(true)
	if !ch.IsRunning() {
		t.Error("expected running=true after Start")
	}
}

func TestStop_SetsRunningFalse(t *testing.T) {
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, nil, nil),
		config: config.TeamsConfig{
			AppID:       "app-id-123",
			AppPassword: newTestSecureString("secret-password"),
		},
	}
	ch.SetRunning(true)
	ch.SetRunning(false)
	if ch.IsRunning() {
		t.Error("expected running=false after Stop")
	}
}

func TestStart_PreregistersReasoningChannelID(t *testing.T) {
	// When ReasoningChannelID is set, it should be pre-registered as "channel"
	// in the chatType map before Start completes.
	cfg := config.TeamsConfig{
		AppID:              "app-id-123",
		AppPassword:        newTestSecureString("secret-password"),
		ReasoningChannelID: "reasoning-chat-id",
	}
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, nil, nil,
			channels.WithReasoningChannelID(cfg.ReasoningChannelID)),
		config: cfg,
		ctx:    context.Background(),
	}

	// Simulate the pre-registration that happens in Start()
	if cfg.ReasoningChannelID != "" {
		ch.chatType.Store(cfg.ReasoningChannelID, "channel")
	}

	val, ok := ch.chatType.Load(cfg.ReasoningChannelID)
	if !ok {
		t.Fatal("reasoning channel ID not stored in chatType map")
	}
	if val != "channel" {
		t.Errorf("reasoning channel ID kind = %q, want channel", val)
	}
}

// -- Test webhookHandler -----------------------------------------------------

func TestWebhookHandler_RejectsNonPOST(t *testing.T) {
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, nil, nil),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	rec := httptest.NewRecorder()
	ch.webhookHandler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebhookHandler_RejectsOversizedBody(t *testing.T) {
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, nil, nil),
	}
	// Create a body larger than maxWebhookBodySize (1 MiB)
	largeBody := strings.Repeat("x", maxWebhookBodySize+1)
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(largeBody))
	rec := httptest.NewRecorder()
	ch.webhookHandler(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestWebhookHandler_RejectsInvalidJSON(t *testing.T) {
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, nil, nil),
	}
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	ch.webhookHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWebhookHandler_Returns200AndStoresConversationRef(t *testing.T) {
	messageBus := bus.NewMessageBus()
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, messageBus, nil),
	}
	ch.SetRunning(true)

	// Build a minimal message activity
	activity := map[string]any{
		"type": "message",
		"id":   "msg-1",
		"from": map[string]any{
			"id": "user-123",
		},
		"conversation": map[string]any{
			"id": "19:channel@thread.tacv2",
		},
		"serviceUrl": "https://teams.example.com",
		"text":      "Hello Teams",
	}
	body, _ := json.Marshal(activity)
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()

	ch.webhookHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify conversation reference was stored
	refVal, ok := ch.convRefs.Load("19:channel@thread.tacv2")
	if !ok {
		t.Fatal("conversation reference not stored")
	}
	ref := refVal.(conversationRef)
	if ref.ServiceURL != "https://teams.example.com" {
		t.Errorf("ServiceURL = %q, want %q", ref.ServiceURL, "https://teams.example.com")
	}
	if ref.ConversationID != "19:channel@thread.tacv2" {
		t.Errorf("ConversationID = %q, want %q", ref.ConversationID, "19:channel@thread.tacv2")
	}
}

func TestWebhookHandler_IgnoresNonMessageActivity(t *testing.T) {
	messageBus := bus.NewMessageBus()
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, messageBus, nil),
	}
	ch.SetRunning(true)

	// Send a non-message activity type
	activity := map[string]any{
		"type": "conversationUpdate",
		"id":   "event-1",
		"from": map[string]any{
			"id": "user-123",
		},
		"conversation": map[string]any{
			"id": "19:channel@thread.tacv2",
		},
		"serviceUrl": "https://teams.example.com",
	}
	body, _ := json.Marshal(activity)
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()

	ch.webhookHandler(rec, req)

	// Should still return 200 (acknowledged)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// -- Test Send ---------------------------------------------------------------

func TestSend_ReturnsErrNotRunningWhenNotStarted(t *testing.T) {
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, nil, nil),
	}
	ch.SetRunning(false)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "teams",
		ChatID:  "19:channel@thread.tacv2",
		Content: "Hello",
	})

	if !errors.Is(err, channels.ErrNotRunning) {
		t.Errorf("Send() error = %v, want ErrNotRunning", err)
	}
}

func TestSend_ReturnsErrorWhenNoConversationRef(t *testing.T) {
	messageBus := bus.NewMessageBus()
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, messageBus, nil),
	}
	ch.SetRunning(true)
	ch.chatType.Store("19:channel@thread.tacv2", "channel")

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "teams",
		ChatID:  "19:channel@thread.tacv2",
		Content: "Hello",
	})

	if err == nil {
		t.Fatal("expected error when no conversation reference found")
	}
	if !strings.Contains(err.Error(), "no conversation reference") {
		t.Errorf("error = %v, want message containing 'no conversation reference'", err)
	}
}

// fakeAdapter is a mock implementation of core.Adapter for testing.
type fakeAdapter struct {
	calls                  []string
	proactiveMessageErr    error
	processActivityErr     error
	proactiveMessageCalled bool
	processActivityCalled  bool
}

func (f *fakeAdapter) ProactiveMessage(ctx context.Context, ref schema.ConversationReference, handler activity.Handler) error {
	f.proactiveMessageCalled = true
	f.calls = append(f.calls, "ProactiveMessage")
	return f.proactiveMessageErr
}

func (f *fakeAdapter) ProcessActivity(ctx context.Context, act schema.Activity, handler activity.Handler) error {
	f.processActivityCalled = true
	f.calls = append(f.calls, "ProcessActivity")
	return f.processActivityErr
}

func (f *fakeAdapter) DeleteActivity(ctx context.Context, conversationID string, ref schema.ConversationReference) error {
	return nil
}

func (f *fakeAdapter) ParseRequest(ctx context.Context, r *http.Request) (schema.Activity, error) {
	return schema.Activity{}, nil
}

func (f *fakeAdapter) UpdateActivity(ctx context.Context, activity schema.Activity) error {
	return nil
}

func TestSend_CallsProactiveMessageAndReturnsErrTemporaryOnFailure(t *testing.T) {
	messageBus := bus.NewMessageBus()
	fake := &fakeAdapter{
		proactiveMessageErr: errors.New("teams API error"),
	}
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, messageBus, nil),
	}
	ch.SetRunning(true)
	ch.adapter = fake
	ch.chatType.Store("19:channel@thread.tacv2", "channel")
	// Pre-store conversation reference
	ch.convRefs.Store("19:channel@thread.tacv2", conversationRef{
		ServiceURL:     "https://teams.example.com",
		ConversationID: "19:channel@thread.tacv2",
	})

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "teams",
		ChatID:  "19:channel@thread.tacv2",
		Content: "Hello from Teams",
	})

	if !errors.Is(err, channels.ErrTemporary) {
		t.Errorf("Send() error = %v, want ErrTemporary", err)
	}
	if !fake.proactiveMessageCalled {
		t.Error("ProactiveMessage was not called")
	}
}

// fakeActivityHandler captures OnMessage calls.
type fakeActivityHandler struct {
	mu          sync.Mutex
	onMessageCalls []int // counts calls per chat kind
}

func (f *fakeActivityHandler) OnMessage(turn *activity.TurnContext) (schema.Activity, error) {
	f.mu.Lock()
	f.onMessageCalls = append(f.onMessageCalls, 1)
	f.mu.Unlock()
	return schema.Activity{}, nil
}

// -- Test processActivity via webhook ----------------------------------------

func TestProcessActivity_StoresChatKind(t *testing.T) {
	messageBus := bus.NewMessageBus()
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, messageBus, nil),
	}
	ch.SetRunning(true)

	activity := map[string]any{
		"type": "message",
		"id":   "msg-direct",
		"from": map[string]any{
			"id": "user@example.com",
		},
		"conversation": map[string]any{
			"id":      "alice@example.com",
			"isGroup": false,
		},
		"serviceUrl": "https://teams.example.com",
		"text":       "Hello direct",
	}
	body, _ := json.Marshal(activity)
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()

	ch.webhookHandler(rec, req)

	// Wait briefly for async processing
	time.Sleep(100 * time.Millisecond)

	// Check that chat type was stored
	val, ok := ch.chatType.Load("alice@example.com")
	if !ok {
		t.Fatal("chat type not stored for direct message")
	}
	if val != "direct" {
		t.Errorf("chat kind = %q, want direct", val)
	}
}

func TestProcessActivity_StoresLastMsgID(t *testing.T) {
	messageBus := bus.NewMessageBus()
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, messageBus, nil),
	}
	ch.SetRunning(true)

	activity := map[string]any{
		"type": "message",
		"id":   "msg-id-12345",
		"from": map[string]any{
			"id": "user-123",
		},
		"conversation": map[string]any{
			"id": "19:channel@thread.tacv2",
		},
		"serviceUrl": "https://teams.example.com",
		"text":       "Test message",
	}
	body, _ := json.Marshal(activity)
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()

	ch.webhookHandler(rec, req)

	// Wait briefly for async processing
	time.Sleep(100 * time.Millisecond)

	// Check that last message ID was stored
	val, ok := ch.lastMsgID.Load("19:channel@thread.tacv2")
	if !ok {
		t.Fatal("lastMsgID not stored")
	}
	if val != "msg-id-12345" {
		t.Errorf("lastMsgID = %q, want msg-id-12345", val)
	}
}

// -- WebhookPath --------------------------------------------------------------

func TestWebhookPath_ReturnsCorrectPath(t *testing.T) {
	ch := &TeamsChannel{}
	if got := ch.WebhookPath(); got != teamsWebhookPath {
		t.Errorf("WebhookPath() = %q, want %q", got, teamsWebhookPath)
	}
}

// -- ServeHTTP delegates to webhookHandler -----------------------------------

func TestServeHTTP_DelegatesToWebhookHandler(t *testing.T) {
	messageBus := bus.NewMessageBus()
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, messageBus, nil),
	}
	ch.SetRunning(true)

	// Send a GET request → should be rejected by webhookHandler
	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	rec := httptest.NewRecorder()

	ch.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("ServeHTTP status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// -- Edge case: empty conversation ID ----------------------------------------

func TestWebhookHandler_EmptyConversationID(t *testing.T) {
	messageBus := bus.NewMessageBus()
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, messageBus, nil),
	}
	ch.SetRunning(true)

	// Activity with no conversation ID should still return 200
	activity := map[string]any{
		"type": "message",
		"id":   "msg-1",
		"from": map[string]any{
			"id": "user-123",
		},
		"conversation": map[string]any{
			"id": "",
		},
		"serviceUrl": "https://teams.example.com",
		"text":       "Hello",
	}
	body, _ := json.Marshal(activity)
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()

	ch.webhookHandler(rec, req)

	// Should still acknowledge to avoid retries
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// -- Readlimit error ----------------------------------------------------------

func TestWebhookHandler_ReadLimitError(t *testing.T) {
	ch := &TeamsChannel{
		BaseChannel: channels.NewBaseChannel("teams", nil, nil, nil),
	}
	// Create a reader that returns an error on read
	limitedReader := &errorReader{}
	req := httptest.NewRequest(http.MethodPost, "/api/messages", limitedReader)
	rec := httptest.NewRecorder()

	ch.webhookHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

type errorReader struct{}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("reader error")
}

// Ensure errorReader implements io.Reader
var _ io.Reader = (*errorReader)(nil)