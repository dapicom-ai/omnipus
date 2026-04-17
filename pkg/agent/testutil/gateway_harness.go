package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/providers"
)

// testMasterKey is the fixed AES key used by all test harnesses.
// It is a 32-byte value encoded as 64 hex characters.
const testMasterKey = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

// testBearerToken is the bearer token injected when WithBearerAuth() is used.
const testBearerToken = "test-bearer-token-for-harness"

// runContextFunc is set by RegisterGatewayRunner. It matches the signature of
// gateway.RunContext so the harness can call the real gateway without importing
// the gateway package (which would create an import cycle).
//
// Signature: func(ctx, debug, homePath, configPath, allowEmpty) error
var runContextFunc func(context.Context, bool, string, string, bool) error

// setProviderOverrideFunc and clearProviderOverrideFunc are set by
// RegisterProviderOverrideFuncs. They match gateway.SetTestProviderOverride
// and gateway.ClearTestProviderOverride.
var (
	setProviderOverrideFunc   func(func() providers.LLMProvider)
	clearProviderOverrideFunc func()
)

// runContextMu guards the registration variables so that tests running in
// parallel do not race on setup (registrations happen once, at test-init time).
var runContextMu sync.RWMutex

// RegisterGatewayRunner installs the gateway.RunContext function so that
// StartTestGateway can call it without importing pkg/gateway. Call this once
// from a TestMain or package-level init in the gateway's test package.
//
// Example (in pkg/gateway/gateway_test_init_test.go):
//
//	func TestMain(m *testing.M) {
//	    testutil.RegisterGatewayRunner(gateway.RunContext)
//	    testutil.RegisterProviderOverrideFuncs(
//	        gateway.SetTestProviderOverride,
//	        gateway.ClearTestProviderOverride,
//	    )
//	    os.Exit(m.Run())
//	}
func RegisterGatewayRunner(fn func(context.Context, bool, string, string, bool) error) {
	runContextMu.Lock()
	defer runContextMu.Unlock()
	runContextFunc = fn
}

// RegisterProviderOverrideFuncs installs the gateway provider-override hooks
// so StartTestGateway can inject a ScenarioProvider without importing pkg/gateway.
// The clearFn parameter name avoids shadowing the builtin identifier "clear".
func RegisterProviderOverrideFuncs(set func(func() providers.LLMProvider), clearFn func()) {
	runContextMu.Lock()
	defer runContextMu.Unlock()
	setProviderOverrideFunc = set
	clearProviderOverrideFunc = clearFn
}

// TestGateway wraps a running gateway for integration tests.
// Cleanup runs automatically via t.Cleanup — callers do not need to call Close.
type TestGateway struct {
	// URL is the base URL of the running gateway, e.g. "http://127.0.0.1:54321".
	URL string

	// HTTPClient is pre-configured with the correct Origin header.
	HTTPClient *http.Client

	// Provider is the ScenarioProvider wired into the gateway agent loop.
	// Tests can script it directly after StartTestGateway returns.
	Provider *ScenarioProvider

	// HomeDir is the temp directory used as OMNIPUS_HOME. Cleaned up automatically.
	HomeDir string

	// ConfigPath is HomeDir/config.json.
	ConfigPath string

	// BearerToken is the token to use for authenticated requests. Empty unless
	// WithBearerAuth() was passed as an option.
	BearerToken string

	// mu guards the closed flag so Close is idempotent.
	mu     sync.Mutex
	closed bool
	cancel context.CancelFunc
	done   chan struct{}

	// t is the test that owns this gateway. Used by Close to report errors.
	t *testing.T

	// bootErr captures any error returned by RunContext so Close can surface it.
	bootErr atomic.Pointer[error]
}

// StartTestGateway boots a real gateway via the registered RunContextFunc on
// an ephemeral port and returns a TestGateway once the /health endpoint
// responds 200.
//
// It requires RegisterGatewayRunner and RegisterProviderOverrideFuncs to have
// been called first (typically from a TestMain in the test package that imports
// pkg/gateway). If neither has been called, StartTestGateway fails the test.
//
// It:
//   - Creates a temp dir for OMNIPUS_HOME via t.TempDir().
//   - Sets OMNIPUS_MASTER_KEY to a fixed test value via t.Setenv.
//   - Picks a free ephemeral port using the listen/close/reuse idiom.
//   - Writes a minimal config.json (gateway.host=127.0.0.1, gateway.port=<port>).
//   - Installs the ScenarioProvider via the registered provider-override hook.
//   - Runs the gateway in a goroutine; captures boot errors.
//   - Polls GET /health until 200 (max 5 s) before returning.
//   - Registers t.Cleanup to call Close, which cancels ctx and waits up to 10 s.
func StartTestGateway(t *testing.T, opts ...Option) *TestGateway {
	t.Helper()

	runContextMu.RLock()
	rcFn := runContextFunc
	setOverride := setProviderOverrideFunc
	clearOverride := clearProviderOverrideFunc
	runContextMu.RUnlock()

	if rcFn == nil {
		t.Fatal("testutil.StartTestGateway: gateway runner not registered — " +
			"call testutil.RegisterGatewayRunner(gateway.RunContext) from TestMain " +
			"before running tests that require the full gateway stack")
	}

	hc := &harnessConfig{
		allowEmpty: true,
	}
	for _, o := range opts {
		o(hc)
	}

	if hc.scenario == nil {
		hc.scenario = NewScenario()
	}

	// Set the master key in the test environment so credentials unlock cleanly.
	t.Setenv("OMNIPUS_MASTER_KEY", testMasterKey)

	// Wire bearer token into the environment so checkBearerAuth's legacy
	// OMNIPUS_BEARER_TOKEN path accepts requests from gw.NewRequest.
	if hc.bearerAuth {
		t.Setenv("OMNIPUS_BEARER_TOKEN", testBearerToken)
	}

	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, "config.json")

	// Pick an ephemeral port by opening a listener, reading the port, then closing it.
	// The OS will not reuse the port immediately, giving RunContext time to bind.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("testutil.StartTestGateway: allocate port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err = ln.Close(); err != nil {
		t.Fatalf("testutil.StartTestGateway: close ephemeral listener: %v", err)
	}

	cfg := buildConfig(hc, homeDir, port)

	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("testutil.StartTestGateway: marshal config: %v", err)
	}
	if err = os.WriteFile(configPath, rawCfg, 0o600); err != nil {
		t.Fatalf("testutil.StartTestGateway: write config: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})

	gw := &TestGateway{
		URL:        baseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		Provider:   hc.scenario,
		HomeDir:    homeDir,
		ConfigPath: configPath,
		cancel:     cancel,
		done:       done,
		t:          t,
	}

	if hc.bearerAuth {
		gw.BearerToken = testBearerToken
	}

	// Install the ScenarioProvider as the gateway's LLM provider via the
	// registered hook. The hook is cleared immediately after the goroutine
	// launches — RunContext reads it synchronously during boot, before the
	// serve loop. This strategy is safe for sequential test runs; if tests run
	// concurrently (t.Parallel + same process), each test's goroutine must have
	// already entered RunContext's boot sequence before the next test clears it.
	// The 5-second /health poll window provides sufficient margin.
	if setOverride != nil {
		scenarioProvider := hc.scenario
		setOverride(func() providers.LLMProvider {
			return scenarioProvider
		})
	}

	go func() {
		defer close(done)
		runErr := rcFn(ctx, false, homeDir, configPath, hc.allowEmpty)
		if runErr != nil {
			gw.bootErr.Store(&runErr)
		}
	}()

	// Poll until /health returns 200 or the deadline expires.
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, httpErr := gw.HTTPClient.Get(baseURL + "/health")
		if httpErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			// Surface any boot error for diagnostics.
			var bootErrMsg string
			if p := gw.bootErr.Load(); p != nil {
				bootErrMsg = fmt.Sprintf(": boot error: %v", *p)
			}
			cancel()
			<-done
			t.Fatalf("testutil.StartTestGateway: gateway at %s did not become ready within 5s%s", baseURL, bootErrMsg)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Clear the provider override now that the gateway is live and has read it.
	// RunContext reads testProviderOverride during boot, before the health
	// endpoint becomes ready. By the time we reach here, the boot path has
	// completed and the override is no longer needed.
	if clearOverride != nil {
		clearOverride()
	}

	t.Cleanup(func() {
		gw.Close()
	})

	return gw
}

// Close stops the gateway. Normally you rely on t.Cleanup; call Close only when
// you need to stop the gateway before the test ends (e.g. restart tests).
// Close is idempotent — calling it multiple times is safe.
//
// Close reports a test failure via t.Errorf if:
//   - RunContext returned a non-nil error after the gateway was considered ready.
//   - The gateway goroutine did not stop within 10 s (goroutine leak).
func (g *TestGateway) Close() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	g.mu.Unlock()

	g.cancel()

	// Wait up to 10 s for RunContext to return. The graceful shutdown sequence
	// in pkg/gateway/shutdown.go has its own 70 s budget, but tests cancel
	// cleanly after in-flight requests drain (which is near-instant in tests).
	select {
	case <-g.done:
	case <-time.After(10 * time.Second):
		if g.t != nil {
			g.t.Errorf("testutil.TestGateway.Close: gateway goroutine did not stop within 10s — goroutine leaked")
		}
		return
	}

	// Surface any boot error that occurred after the gateway became ready.
	if p := g.bootErr.Load(); p != nil && *p != nil {
		if g.t != nil {
			g.t.Errorf("testutil.TestGateway.Close: gateway exited with error: %v", *p)
		}
	}
}

// NewRequest builds an *http.Request with the path prefixed to g.URL,
// the Origin header set to g.URL, and (if BearerToken is non-empty) the
// Authorization header set.
func (g *TestGateway) NewRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, g.URL+path, body)
	if err != nil {
		return nil, fmt.Errorf("testutil.TestGateway.NewRequest: %w", err)
	}
	req.Header.Set("Origin", g.URL)
	if g.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+g.BearerToken)
	}
	return req, nil
}

// Do sends req via g.HTTPClient. Returns (nil, err) on network error.
func (g *TestGateway) Do(req *http.Request) (*http.Response, error) {
	return g.HTTPClient.Do(req)
}

// buildConfig assembles a minimal config.Config from the harness options.
func buildConfig(hc *harnessConfig, homeDir string, port int) *config.Config {
	cfg := &config.Config{
		Version: 1,
		Gateway: config.GatewayConfig{
			Host:      "127.0.0.1",
			Port:      port,
			HotReload: false,
		},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: homeDir,
				ModelName: "scripted-model",
				MaxTokens: 4096,
			},
		},
	}

	if len(hc.agents) > 0 {
		cfg.Agents.List = hc.agents
	}

	if hc.sandbox != nil {
		cfg.Sandbox = *hc.sandbox
	}

	if hc.bearerAuth {
		// Store the raw token so the withAuth middleware accepts it via the
		// Authorization: Bearer header. Dev mode bypass is left false so that
		// auth is actually enforced.
		cfg.Gateway.Token = testBearerToken
	} else {
		// Allow unauthenticated access for tests that do not need auth.
		cfg.Gateway.DevModeBypass = true
	}

	return cfg
}
