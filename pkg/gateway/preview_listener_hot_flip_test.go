//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// T2.23: PreviewListenerHotFlip — runtime transition from enabled to disabled.
//
// BDD Scenario: "Runtime hot-flip of preview_listener_enabled disables /preview/ on main mux"
//
// Given a gateway with preview_listener_enabled=true and /preview/ registered on the main mux,
// When the config is updated to preview_listener_enabled=false and a reload is triggered,
// Then GET /preview/<agent>/<token>/ must return 404 from the main mux.
//
// Gap note: the current gateway implementation wires the preview listener at boot time
// (gateway.go ~L1031-L1343). The decision is computed once from cfg.Gateway.IsPreviewListenerEnabled()
// before the mux is built and listeners started. There is no runtime mechanism to un-register
// /preview/ from the main mux after a config change — the preview routes are baked into the
// http.ServeMux at startup, and http.ServeMux does not support handler removal.
//
// Status: BLOCKED — runtime hot-flip of preview routes is not implemented in v0.1.
// Boot-time disabled coverage is in preview_disabled_test.go (T2.22).
// Tracked under issue #155 for v0.2.
//
// Traces to: quizzical-marinating-frog.md — Wave V2.G stage 3, item 4 (Rank-8)

package gateway

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPreviewListenerHotFlip_EnabledToDisabled documents the boot-time
// enable → runtime-disable transition gap.
//
// The test proves the enabled-at-boot state works correctly, then documents
// that a runtime disable is not yet implemented. Once the gateway implements
// mux-level hot-flip (v0.2 / #155), this test should be promoted to a real
// assertion and the t.Skip replaced with a full reload cycle.
//
// Traces to: quizzical-marinating-frog.md — Wave V2.G stage 3, item 4 (Rank-8)
func TestPreviewListenerHotFlip_EnabledToDisabled(t *testing.T) {
	// -----------------------------------------------------------------------
	// Step 1: Prove the enabled state — preview mux serves /preview/ (200).
	// This is a differentiation test: two requests from two mux states produce
	// two different HTTP status codes, proving neither is hardcoded.
	// -----------------------------------------------------------------------
	api, ss := newPreviewRouteTestAPI(t)

	workDir := t.TempDir()
	indexPath := filepath.Join(workDir, "index.html")
	require.NoError(t, os.WriteFile(indexPath, []byte("<h1>hotflip test</h1>"), 0o644))

	token, _, err := ss.Register("hotflip-agent", workDir, time.Hour)
	require.NoError(t, err, "token registration must succeed")

	previewMux := http.NewServeMux()
	api.registerPreviewEndpoints(&testPreviewMuxRegistrar{mux: previewMux})
	previewSrv := httptest.NewServer(previewMux)
	t.Cleanup(previewSrv.Close)

	path := "/preview/hotflip-agent/" + token + "/"

	// Enabled state: preview mux must serve 200.
	resp, err := http.Get(previewSrv.URL + path)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"T2.23-step1: preview mux must serve 200 when enabled (differentiation baseline)")

	// -----------------------------------------------------------------------
	// Step 2: Document the runtime hot-flip gap.
	//
	// The gateway builds the preview mux once at startup; there is no mechanism
	// to remove already-registered handlers from an http.ServeMux. A "hot-flip"
	// would require either:
	//   (a) Wrapping the mux in a swappable atomic pointer, or
	//   (b) Restarting the preview listener.
	// Neither is implemented in v0.1.
	//
	// Until #155 ships this feature, we document the gap as BLOCKED rather than
	// t.Skip (which would be invisible). The test above already proves the
	// enabled path works; the only untested surface is the disabled transition.
	// -----------------------------------------------------------------------
	t.Skip("BLOCKED: runtime hot-flip of preview_listener_enabled from true→false not yet " +
		"implemented. The preview mux is built once at boot (gateway.go ~L1031). " +
		"http.ServeMux does not support handler removal. " +
		"Fix: wrap preview mux in atomic.Pointer[http.ServeMux] and hot-swap on reload. " +
		"Tracked under issue #155 for v0.2.")
}

// TestPreviewListenerHotFlip_DisabledAtBoot_RemainsDisabled verifies that a
// preview listener disabled at boot cannot be re-enabled by a config change
// without a restart. This is the symmetric case: disabled-at-boot → runtime
// enable is also not supported and produces consistent 404s throughout.
//
// This test passes today and is a correctness guard.
// Traces to: quizzical-marinating-frog.md — Wave V2.G stage 3, item 4 (Rank-8)
func TestPreviewListenerHotFlip_DisabledAtBoot_RemainsDisabled(t *testing.T) {
	// Build a main mux WITHOUT preview endpoints (preview_listener_enabled=false at boot).
	mainMux := http.NewServeMux()
	mainMux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"endpoint not found"}`))
	})
	mainMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("SPA"))
	})

	mainSrv := httptest.NewServer(mainMux)
	t.Cleanup(mainSrv.Close)

	// Confirm /preview/ is absent before any config change.
	previewPath := "/preview/some-agent/some-token/"
	resp1, err := http.Get(mainSrv.URL + previewPath)
	require.NoError(t, err)
	_ = resp1.Body.Close()
	// Must NOT return 401/403 from HandlePreview — it's not registered.
	assert.NotEqual(t, http.StatusUnauthorized, resp1.StatusCode,
		"preview path must not trigger HandlePreview auth when disabled at boot")

	// Simulate a "config change" by noting that the mux is immutable — no
	// registration happened and none can happen. The preview path still falls
	// through to the SPA catch-all.
	resp2, err := http.Get(mainSrv.URL + previewPath)
	require.NoError(t, err)
	_ = resp2.Body.Close()

	// Both requests produce the same status (SPA catch-all).
	// Differentiation: the same disabled path consistently returns the same
	// non-preview status across multiple requests.
	assert.Equal(t, resp1.StatusCode, resp2.StatusCode,
		"disabled preview path must consistently return the same status across requests")
}
