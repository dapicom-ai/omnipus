//go:build !cgo

// helpers_test.go provides perf-package-local helpers for all benchmark files.
//
// startPerfGateway mirrors testutil.StartTestGateway but accepts testing.TB so
// it can be called from both *testing.T (SLO tests) and *testing.B (benchmarks).
// It boots the real gateway via gateway.RunContext (registered in TestMain) with
// DevModeBypass=true and a ScenarioProvider so there are no external LLM calls.
package perf

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/agent/testutil"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/gateway"
	"github.com/dapicom-ai/omnipus/pkg/providers"
)

// testMasterKey matches the value in pkg/agent/testutil/gateway_harness.go.
const testMasterKey = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

// perfGateway is a lightweight wrapper around a running gateway for perf tests.
type perfGateway struct {
	URL        string
	HTTPClient *http.Client
	cancel     context.CancelFunc
	done       chan struct{}
	bootErr    atomic.Pointer[error]
}

// close stops the gateway and waits up to 10 s for shutdown.
func (g *perfGateway) close(tb testing.TB) {
	tb.Helper()
	g.cancel()
	select {
	case <-g.done:
	case <-time.After(10 * time.Second):
		tb.Errorf("perfGateway.close: gateway goroutine did not stop within 10 s — goroutine leaked")
	}
	if p := g.bootErr.Load(); p != nil && *p != nil {
		tb.Errorf("perfGateway.close: gateway exited with error: %v", *p)
	}
}

// startPerfGateway boots a real gateway for perf tests. It accepts testing.TB
// so it works from both *testing.T (SLO tests) and *testing.B (benchmarks).
//
// The scenario parameter may be nil; in that case an empty ScenarioProvider is used.
// DevModeBypass is always true (no bearer auth required for WS connections).
func startPerfGateway(tb testing.TB, scenario *testutil.ScenarioProvider) *perfGateway {
	tb.Helper()

	if scenario == nil {
		scenario = testutil.NewScenario()
	}

	// Set master key so credentials unlock cleanly.
	tb.Setenv("OMNIPUS_MASTER_KEY", testMasterKey)
	// Clear bearer token so DevModeBypass takes effect.
	tb.Setenv("OMNIPUS_BEARER_TOKEN", "")

	homeDir := tb.TempDir()
	configPath := filepath.Join(homeDir, "config.json")

	// Pick an ephemeral port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("startPerfGateway: allocate port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err = ln.Close(); err != nil {
		tb.Fatalf("startPerfGateway: close ephemeral listener: %v", err)
	}

	cfg := &config.Config{
		Version: 1,
		Gateway: config.GatewayConfig{
			Host:          "127.0.0.1",
			Port:          port,
			HotReload:     false,
			DevModeBypass: true,
		},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: homeDir,
				ModelName: "scripted-model",
				MaxTokens: 4096,
			},
		},
	}

	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		tb.Fatalf("startPerfGateway: marshal config: %v", err)
	}
	if err = os.WriteFile(configPath, rawCfg, 0o600); err != nil {
		tb.Fatalf("startPerfGateway: write config: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	gw := &perfGateway{
		URL:        baseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		cancel:     cancel,
		done:       done,
	}

	// Install the scenario provider before launching RunContext.
	// gateway.SetTestProviderOverride is safe to call from any goroutine;
	// RunContext reads it synchronously during boot before the serve loop starts.
	scenarioCopy := scenario
	gateway.SetTestProviderOverride(func() providers.LLMProvider {
		return scenarioCopy
	})

	go func() {
		defer close(done)
		runErr := gateway.RunContext(ctx, false, homeDir, configPath, true /*allowEmpty*/)
		if runErr != nil {
			gw.bootErr.Store(&runErr)
		}
	}()

	// Poll /health until 200 or 5 s timeout.
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
			var bootErrMsg string
			if p := gw.bootErr.Load(); p != nil {
				bootErrMsg = fmt.Sprintf(": boot error: %v", *p)
			}
			cancel()
			<-done
			tb.Fatalf("startPerfGateway: gateway at %s did not become ready within 5 s%s", baseURL, bootErrMsg)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Clear the provider override now that the gateway has booted and consumed it.
	gateway.ClearTestProviderOverride()

	tb.Cleanup(func() { gw.close(tb) })
	return gw
}
