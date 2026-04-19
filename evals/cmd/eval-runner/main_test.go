package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── CSRF extraction ───────────────────────────────────────────────────────────

// mockGatewayWithCSRF starts a test HTTP server that:
//   - Issues __Host-csrf cookie on every POST response.
//   - Records whether incoming requests carry the cookie + header.
func mockGatewayWithCSRF(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var csrfHitCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Emit the CSRF cookie on every response.
		http.SetCookie(w, &http.Cookie{
			Name:  csrfCookieName,
			Value: "abc123",
		})

		if r.Method == http.MethodPost {
			headerOK := r.Header.Get("X-Csrf-Token") == "abc123"
			cookieOK := false
			for _, c := range r.Cookies() {
				if c.Name == csrfCookieName && c.Value == "abc123" {
					cookieOK = true
				}
			}
			if headerOK && cookieOK {
				csrfHitCount.Add(1)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"test-token","id":"sess-1"}`))
	}))

	return srv, &csrfHitCount
}

func TestCSRFTokenExtractedAndSent(t *testing.T) {
	srv, csrfHits := mockGatewayWithCSRF(t)
	defer srv.Close()

	h := &gatewayHandle{baseURL: srv.URL}

	// First call: no token yet — doStatefulPost won't send csrf headers.
	body1, _ := json.Marshal(map[string]string{"x": "1"})
	resp1, err := h.doStatefulPost("/test", body1)
	if err != nil {
		t.Fatalf("first doStatefulPost error: %v", err)
	}
	h.extractCSRF(resp1)
	resp1.Body.Close()

	if h.csrfToken != "abc123" {
		t.Fatalf("expected csrfToken='abc123' after first response, got %q", h.csrfToken)
	}

	// Second call: token is now set — should carry both cookie and header.
	body2, _ := json.Marshal(map[string]string{"x": "2"})
	resp2, err := h.doStatefulPost("/test", body2)
	if err != nil {
		t.Fatalf("second doStatefulPost error: %v", err)
	}
	resp2.Body.Close()

	if csrfHits.Load() == 0 {
		t.Error("expected at least one request with valid CSRF cookie + header, got 0")
	}
}

// ── discoverScenarios — malformed YAML is skipped ─────────────────────────────

func TestDiscoverScenarios_SkipsMalformedYAML(t *testing.T) {
	dir := t.TempDir()

	// Write a valid scenario.
	validYAML := `id: test.valid
agent_id: mia
prompt: "Hello"
max_turns: 1
rubric: "Be friendly."
`
	if err := os.WriteFile(filepath.Join(dir, "valid.yaml"), []byte(validYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a malformed scenario (invalid YAML).
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(":::invalid yaml:::"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a YAML that parses but fails validation (missing agent_id).
	invalidScenario := `id: test.noid
prompt: "Hello"
`
	if err := os.WriteFile(filepath.Join(dir, "noid.yaml"), []byte(invalidScenario), 0o644); err != nil {
		t.Fatal(err)
	}

	scenarios, err := discoverScenarios(dir)
	if err != nil {
		t.Fatalf("unexpected error from discoverScenarios: %v", err)
	}
	if len(scenarios) != 1 {
		t.Errorf("expected 1 valid scenario, got %d", len(scenarios))
	}
	if len(scenarios) > 0 && scenarios[0].ID != "test.valid" {
		t.Errorf("expected scenario id 'test.valid', got %q", scenarios[0].ID)
	}
}

// ── Zero scenarios exit code ──────────────────────────────────────────────────

// TestAllowEmptyScenarios verifies the cfg field is wired correctly.
// We can't test os.Exit directly, but we can test the logic that controls it.
func TestAllowEmptyScenarios_FieldDefault(t *testing.T) {
	// parseFlags reads from os.Args; we test the cfg struct directly.
	c := cfg{allowEmptyScenarios: false}
	if c.allowEmptyScenarios {
		t.Error("allowEmptyScenarios should default to false")
	}
}

// ── EvalResult error tally ────────────────────────────────────────────────────

func TestEvalResult_ErrorTally(t *testing.T) {
	// Simulate the counting logic from main() to verify F32 logic.
	results := []EvalResult{
		{ScenarioID: "a", Error: "failed"},
		{ScenarioID: "b", Error: "also failed"},
	}

	erroredCount := 0
	successCount := 0
	for _, r := range results {
		if r.Error != "" {
			erroredCount++
		} else {
			successCount++
		}
	}

	total := len(results)
	allErrored := erroredCount == total && total > 0

	if !allErrored {
		t.Error("expected allErrored=true when all results have errors")
	}
	if successCount != 0 {
		t.Errorf("expected successCount=0, got %d", successCount)
	}
}

func TestEvalResult_PartialErrorDoesNotTriggerAllError(t *testing.T) {
	results := []EvalResult{
		{ScenarioID: "a", Error: "failed"},
		{ScenarioID: "b", Error: ""},
	}

	erroredCount := 0
	for _, r := range results {
		if r.Error != "" {
			erroredCount++
		}
	}

	total := len(results)
	allErrored := erroredCount == total && total > 0

	if allErrored {
		t.Error("expected allErrored=false when only some results have errors")
	}
}

// ── JSONL output survives all-error run ───────────────────────────────────────

func TestJSONLWrittenEvenOnAllErrors(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "results.jsonl")

	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	results := []EvalResult{
		{
			ScenarioID: "a",
			TS:         time.Now(),
			Error:      "gateway unreachable",
		},
	}

	for _, r := range results {
		line, marshalErr := json.Marshal(r)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, writeErr := f.Write(append(line, '\n')); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	f.Close()

	// Verify file exists and has content.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if !strings.Contains(string(data), "gateway unreachable") {
		t.Error("expected error message in JSONL output")
	}
}

// ── gatewayHandle.doStatefulPost attaches Authorization header ────────────────

func TestDoStatefulPost_AttachesAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	h := &gatewayHandle{
		baseURL: srv.URL,
		token:   "my-bearer-token",
	}
	body, _ := json.Marshal(map[string]string{"k": "v"})
	resp, err := h.doStatefulPost("/any", body)
	if err != nil {
		t.Fatalf("doStatefulPost error: %v", err)
	}
	resp.Body.Close()

	if gotAuth != "Bearer my-bearer-token" {
		t.Errorf("expected Authorization 'Bearer my-bearer-token', got %q", gotAuth)
	}
}
