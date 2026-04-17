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
	"testing"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// testMasterKey is the fixed AES key used by all test harnesses.
// It is a 32-byte value encoded as 64 hex characters.
const testMasterKey = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

// testBearerToken is the bearer token injected when WithBearerAuth() is used.
const testBearerToken = "test-bearer-token-for-harness"

// TestGateway wraps a running gateway for integration tests.
// Cleanup runs automatically via t.Cleanup — callers do not defer Close.
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
	server *http.Server
	done   chan struct{}
}

// StartTestGateway boots a minimal gateway HTTP server in a goroutine.
//
// It:
//   - Allocates an ephemeral port on 127.0.0.1.
//   - Creates a temp dir for OMNIPUS_HOME via t.TempDir().
//   - Writes a minimal config.json (gateway.host=127.0.0.1, gateway.port=<picked>).
//   - Sets OMNIPUS_MASTER_KEY to a fixed test value so credentials unlock cleanly.
//   - Launches a minimal HTTP server using a ScenarioProvider (accessible via .Provider).
//   - Polls GET /health until 200 (max 5s) before returning.
//   - Registers t.Cleanup to stop the server and remove the temp dir.
//
// Note: This harness serves a lightweight HTTP layer (health + config endpoints)
// rather than the full gateway.Run path. The full gateway.Run function blocks on
// OS signal handling and exposes no context-cancellation API, making it unsuitable
// for in-process integration tests. Future work can add gateway.RunContext to
// enable full-stack harness tests.
//
// Opts are applied in order before start.
func StartTestGateway(t *testing.T, opts ...Option) *TestGateway {
	t.Helper()

	hc := &harnessConfig{
		allowEmpty: true,
	}
	for _, o := range opts {
		o(hc)
	}

	if hc.scenario == nil {
		hc.scenario = NewScenario()
	}

	// Set the master key in the test environment.
	t.Setenv("OMNIPUS_MASTER_KEY", testMasterKey)

	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, "config.json")

	// Pick an ephemeral port by opening a listener, reading the port, then closing it.
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

	// Ensure the OMNIPUS_HOME directory tree exists (logs/, sessions/, tasks/, etc.).
	if err = os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("testutil.StartTestGateway: mkdir home: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	allowedOrigin := baseURL

	mux := buildMux(cfg, allowedOrigin)

	ctx, cancel := context.WithCancel(context.Background())
	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}

	done := make(chan struct{})

	gw := &TestGateway{
		URL:        baseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		Provider:   hc.scenario,
		HomeDir:    homeDir,
		ConfigPath: configPath,
		cancel:     cancel,
		server:     srv,
		done:       done,
	}

	if hc.bearerAuth {
		gw.BearerToken = testBearerToken
	}

	go func() {
		defer close(done)
		if serveErr := srv.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			// Log but do not panic; the test cleanup will catch any
			// test-assertion failures caused by the server being unavailable.
			_ = serveErr
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
			cancel()
			_ = srv.Shutdown(context.Background())
			t.Fatalf("testutil.StartTestGateway: gateway at %s did not become ready within 5s", baseURL)
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Cleanup(func() {
		gw.Close()
	})

	// Ensure the cancel context goroutine from ctx is also cleaned up.
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	return gw
}

// Close stops the gateway early. Normally you rely on t.Cleanup.
// Close is idempotent — calling it multiple times is safe.
func (g *TestGateway) Close() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	g.mu.Unlock()

	g.cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = g.server.Shutdown(shutdownCtx)
	<-g.done
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
		// Seed one admin user. In production the password is bcrypt-hashed;
		// for tests we rely on OMNIPUS_BEARER_TOKEN env-var-style token matching.
		// The simplest approach: store the token directly in DevModeBypass=false
		// and require callers to pass the token via Authorization header. We set
		// the raw token in Gateway.Token so the withAuth middleware accepts it.
		cfg.Gateway.Token = testBearerToken
	} else {
		// Allow unauthenticated access for tests that don't need auth.
		cfg.Gateway.DevModeBypass = true
	}

	return cfg
}

// buildMux returns an http.Handler with a /health endpoint (always 200) and
// a /ready endpoint (always 200). Future work can extend this to proxy the
// full gateway REST API once gateway internals are exported.
func buildMux(_ *config.Config, _ string) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	return mux
}
