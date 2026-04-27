// Package browser implements browser automation tools using chromedp (pure Go CDP).
//
// Implements US-4 (managed mode), US-6 (remote CDP mode), US-7 (resource limits)
// from the Wave 4 spec. All navigations are SSRF-checked via pkg/security.SSRFChecker.
package browser

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dapicom-ai/omnipus/pkg/logger"
	"github.com/dapicom-ai/omnipus/pkg/security"
)

// BrowserConfig holds browser automation configuration.
// Mapped from config.json: tools.browser.*
type BrowserConfig struct {
	Enabled        bool          `json:"enabled"`
	Headless       bool          `json:"headless"`
	CDPURL         string        `json:"cdp_url"`         // Remote CDP WebSocket URL (US-6)
	PageTimeout    time.Duration `json:"page_timeout"`    // Per-page load timeout (US-7, default 30s)
	MaxTabs        int           `json:"max_tabs"`        // Max concurrent tabs (US-7, default 5)
	PersistSession bool          `json:"persist_session"` // Persist cookies/localStorage across restarts
	ProfileDir     string        `json:"profile_dir"`     // User data dir (default ~/.omnipus/browser/profiles/default/)
}

// DefaultConfig returns a BrowserConfig with spec-defined defaults.
// Returns an error if the user home directory cannot be determined.
func DefaultConfig() (BrowserConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return BrowserConfig{}, fmt.Errorf("browser: cannot determine home directory: %w", err)
	}
	return BrowserConfig{
		Enabled:     false,
		Headless:    true,
		PageTimeout: 30 * time.Second,
		MaxTabs:     5,
		ProfileDir:  filepath.Join(homeDir, ".omnipus", "browser", "profiles", "default"),
	}, nil
}

// sessionEntry tracks a chromedp tab context that persists across tool calls.
type sessionEntry struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// BrowserManager manages the Chromium lifecycle and tab pool.
// Thread-safe — all methods may be called concurrently.
//
// Session model: tools operate on a persistent "default" session tab so that
// browser.navigate, browser.click, browser.get_text, etc. act on the same page.
// Additional sessions can be created for parallel browsing up to MaxTabs.
type BrowserManager struct {
	cfg         BrowserConfig
	ssrf        *security.SSRFChecker // never nil — enforced by NewBrowserManager
	mu          sync.Mutex
	allocCtx    context.Context    // chromedp allocator context
	allocCancel context.CancelFunc // cancels the allocator (kills browser)
	sessions    map[string]*sessionEntry
	started     bool
}

// NewBrowserManager creates a manager. ssrf must be non-nil — SSRF protection
// is mandatory for browser tools (SEC-24). The browser is not launched until
// the first tool invocation (lazy init).
func NewBrowserManager(cfg BrowserConfig, ssrf *security.SSRFChecker) (*BrowserManager, error) {
	if ssrf == nil {
		return nil, fmt.Errorf(
			"browser: SSRFChecker is required — cannot create browser manager without SSRF protection (SEC-24)",
		)
	}
	return &BrowserManager{
		cfg:      cfg,
		ssrf:     ssrf,
		sessions: make(map[string]*sessionEntry),
	}, nil
}

// blockedSchemes are URL schemes that bypass network-level SSRF and must be
// denied at the application layer. file:// would bypass Landlock restrictions.
var blockedSchemes = map[string]bool{
	"file":             true,
	"javascript":       true,
	"data":             true,
	"chrome":           true,
	"chrome-extension": true,
}

// ValidateURL checks a URL against SSRF rules and blocked schemes.
// Returns an error if navigation should be denied.
func (m *BrowserManager) ValidateURL(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("browser: invalid URL %q: %w", rawURL, err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" {
		return fmt.Errorf("browser: URL %q has no scheme — use http:// or https://", rawURL)
	}
	if blockedSchemes[scheme] {
		return fmt.Errorf("browser: %s:// URLs are blocked for security reasons", scheme)
	}
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("browser: only http:// and https:// URLs are permitted, got %s://", scheme)
	}

	// SSRF check: resolve host, block private IPs and cloud metadata (SEC-24)
	if err := m.ssrf.CheckURL(ctx, rawURL); err != nil {
		return fmt.Errorf("browser: navigation blocked by SSRF policy: %w", err)
	}

	return nil
}

// ensureStarted lazily initializes the browser. Must be called under m.mu.
func (m *BrowserManager) ensureStarted() error {
	if m.started {
		return nil
	}

	if m.cfg.CDPURL != "" {
		// US-6: Remote CDP mode — connect to external Chromium
		allocCtx, cancel := chromedp.NewRemoteAllocator(context.Background(), m.cfg.CDPURL)
		m.allocCtx = allocCtx
		m.allocCancel = cancel
		m.started = true
		logger.InfoCF("browser", "Connected to remote CDP", map[string]any{
			"url": m.cfg.CDPURL,
		})
		return nil
	}

	// US-4: Managed mode — launch local Chromium
	if err := os.MkdirAll(m.cfg.ProfileDir, 0o700); err != nil {
		return fmt.Errorf("browser: cannot create profile directory %s: %w", m.cfg.ProfileDir, err)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(m.cfg.ProfileDir),
		chromedp.DisableGPU,
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.WindowSize(1280, 720),
	)

	if m.cfg.Headless {
		opts = append(opts, chromedp.Headless)
	}

	// GitHub Actions runners restrict unprivileged user namespaces via AppArmor,
	// which breaks Chromium's zygote sandbox. Opt into --no-sandbox when CI=true
	// or OMNIPUS_BROWSER_NO_SANDBOX=1 is set explicitly. Not enabled by default
	// because --no-sandbox reduces isolation for code the browser loads.
	if os.Getenv("CI") == "true" || os.Getenv("OMNIPUS_BROWSER_NO_SANDBOX") == "1" {
		opts = append(opts, chromedp.NoSandbox)
	}

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	m.allocCtx = allocCtx
	m.allocCancel = cancel
	m.started = true

	logger.InfoCF("browser", "Browser allocator ready (managed mode)", map[string]any{
		"headless":    m.cfg.Headless,
		"profile_dir": m.cfg.ProfileDir,
	})
	return nil
}

// Session returns a persistent tab context for the given session ID.
// If the session does not exist, a new tab is created (subject to MaxTabs).
// The "default" session is used by all tools unless a session_id is specified.
func (m *BrowserManager) Session(sessionID string) (context.Context, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureStarted(); err != nil {
		return nil, err
	}

	if s, ok := m.sessions[sessionID]; ok {
		// Verify the session context is still valid
		if s.ctx.Err() == nil {
			return s.ctx, nil
		}
		// Session expired (browser crash, etc.) — clean up and recreate
		s.cancel()
		delete(m.sessions, sessionID)
	}

	if len(m.sessions) >= m.cfg.MaxTabs {
		return nil, fmt.Errorf("maximum concurrent tabs (%d) reached. Close a tab first", m.cfg.MaxTabs)
	}

	ctx, cancel := chromedp.NewContext(m.allocCtx)
	// Eagerly create the target on this ctx. Without this, the first
	// chromedp.Run binds the target to whichever (possibly timeout-wrapped)
	// ctx a tool passes — and when that wrapper is canceled, the tab dies.
	// The next tool call then silently creates a fresh blank tab, so e.g.
	// screenshot-after-navigate returns a blank page.
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("browser: failed to initialize tab: %w", err)
	}
	m.sessions[sessionID] = &sessionEntry{ctx: ctx, cancel: cancel}
	return ctx, nil
}

// CloseSession closes a specific session tab.
func (m *BrowserManager) CloseSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[sessionID]; ok {
		s.cancel()
		delete(m.sessions, sessionID)
	}
}

// PageTimeout returns the configured page load timeout.
func (m *BrowserManager) PageTimeout() time.Duration {
	return m.cfg.PageTimeout
}

// Shutdown gracefully shuts down the browser process and all sessions.
func (m *BrowserManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, s := range m.sessions {
		s.cancel()
		delete(m.sessions, id)
	}

	if m.allocCancel != nil {
		m.allocCancel()
		m.allocCancel = nil
	}
	m.started = false

	logger.InfoCF("browser", "Browser shut down", nil)
}
