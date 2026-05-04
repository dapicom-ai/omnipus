// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Env-scrub test for the ScrubGatewayEnv / scrubGatewayEnv path on all platforms.
//
// CLAUDE.md hard constraint #4: graceful degradation is required on non-Linux.
// The existing hardened_exec_env_test.go covers Linux env-scrubbing on the
// hardened-exec path. This test covers the ScrubGatewayEnv exported function
// that is the cross-platform scrub primitive.
//
// BDD Scenario: "ScrubGatewayEnv strips OMNIPUS_MASTER_KEY, OMNIPUS_BEARER_TOKEN,
//               OMNIPUS_KEY_FILE from the child environment and preserves PATH"
//
// Given the parent process has OMNIPUS_MASTER_KEY, OMNIPUS_BEARER_TOKEN, and
//   OMNIPUS_KEY_FILE set in its environment,
// When ScrubGatewayEnv() is called,
// Then the returned slice contains zero entries with those keys,
//   and PATH is still present.
//
// Traces to: quizzical-marinating-frog.md — Wave V2.G stage 3, item 6 (Rank-8)

package sandbox

import (
	"os"
	"strings"
	"testing"
)

// TestScrubGatewayEnv_StripsSensitiveKeys verifies that ScrubGatewayEnv
// strips the three sensitive gateway env keys and preserves other env vars.
//
// This test exercises the cross-platform scrub primitive (scrubGatewayEnv)
// which underlies both the Linux hardened-exec path and the fallback path.
//
// Differentiation: three distinct sensitive keys are set and all three must
// be absent; an unrelated key (OMNIPUS_TEST_SAFE_KEY) must be preserved.
//
// Traces to: quizzical-marinating-frog.md — Wave V2.G stage 3, item 6 (Rank-8)
func TestScrubGatewayEnv_StripsSensitiveKeys(t *testing.T) {
	// Set all three sensitive keys in the parent env.
	t.Setenv("OMNIPUS_MASTER_KEY", "secret123")
	t.Setenv("OMNIPUS_BEARER_TOKEN", "tok-deadbeef")
	t.Setenv("OMNIPUS_KEY_FILE", "/etc/omnipus/master.key")
	// Set a safe key that must survive the scrub.
	t.Setenv("OMNIPUS_TEST_SAFE_KEY", "safe-value-preserved")

	scrubbed := ScrubGatewayEnv()

	// Assert: no sensitive key must appear in the scrubbed env.
	sensitiveKeys := []string{
		"OMNIPUS_MASTER_KEY",
		"OMNIPUS_BEARER_TOKEN",
		"OMNIPUS_KEY_FILE",
	}
	for _, key := range sensitiveKeys {
		for _, kv := range scrubbed {
			eq := strings.IndexByte(kv, '=')
			if eq <= 0 {
				continue
			}
			k := kv[:eq]
			if k == key {
				t.Errorf("ScrubGatewayEnv: sensitive key %q must be stripped, but found %q in scrubbed env", key, kv)
			}
		}
	}

	// Assert: safe key must be preserved (scrub must not wipe non-sensitive keys).
	var safeFound bool
	for _, kv := range scrubbed {
		if strings.HasPrefix(kv, "OMNIPUS_TEST_SAFE_KEY=") {
			safeFound = true
			break
		}
	}
	if !safeFound {
		t.Error("ScrubGatewayEnv: non-sensitive key OMNIPUS_TEST_SAFE_KEY must be preserved, but was not found")
	}
}

// TestScrubGatewayEnv_PreservesPATH verifies that PATH is present in the
// scrubbed environment. Without PATH, children cannot locate executables.
//
// Traces to: quizzical-marinating-frog.md — Wave V2.G stage 3, item 6 (Rank-8)
func TestScrubGatewayEnv_PreservesPATH(t *testing.T) {
	// Only run if PATH is actually set (it is on all Unix systems; on Windows
	// the equivalent is Path — check both).
	originalPath := os.Getenv("PATH")
	if originalPath == "" {
		originalPath = os.Getenv("Path") // Windows
	}
	if originalPath == "" {
		t.Skip("PATH not set in this environment — cannot assert preservation")
	}

	scrubbed := ScrubGatewayEnv()

	var pathFound bool
	for _, kv := range scrubbed {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		k := kv[:eq]
		if strings.EqualFold(k, "PATH") {
			pathFound = true
			break
		}
	}
	if !pathFound {
		t.Error("ScrubGatewayEnv: PATH must be present in scrubbed env so children can locate executables")
	}
}

// TestScrubGatewayEnv_SensitiveKeysAbsentWhenUnset verifies that the scrub
// works correctly even when the sensitive keys are not present in the parent env.
// This prevents a regression where a missing key causes a panic or corrupted output.
//
// Traces to: quizzical-marinating-frog.md — Wave V2.G stage 3, item 6 (Rank-8)
func TestScrubGatewayEnv_SensitiveKeysAbsentWhenUnset(t *testing.T) {
	// Ensure the sensitive keys are NOT set.
	t.Setenv("OMNIPUS_MASTER_KEY", "")
	t.Setenv("OMNIPUS_BEARER_TOKEN", "")
	t.Setenv("OMNIPUS_KEY_FILE", "")
	os.Unsetenv("OMNIPUS_MASTER_KEY")
	os.Unsetenv("OMNIPUS_BEARER_TOKEN")
	os.Unsetenv("OMNIPUS_KEY_FILE")

	// Must not panic.
	var scrubbed []string
	require := func(cond bool, msg string) {
		t.Helper()
		if !cond {
			t.Error(msg)
		}
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ScrubGatewayEnv panicked with no sensitive keys in env: %v", r)
			}
		}()
		scrubbed = ScrubGatewayEnv()
	}()

	// Must return a non-nil slice.
	require(scrubbed != nil, "ScrubGatewayEnv must return a non-nil slice even when no sensitive keys are set")

	// No sensitive key should appear (keys were unset so they shouldn't appear
	// unless os.Setenv above didn't clear them — belt and suspenders).
	for _, kv := range scrubbed {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		k := kv[:eq]
		switch k {
		case "OMNIPUS_MASTER_KEY", "OMNIPUS_BEARER_TOKEN", "OMNIPUS_KEY_FILE":
			t.Errorf("ScrubGatewayEnv: found sensitive key %q with empty value — should be stripped", k)
		}
	}
}

// TestScrubGatewayEnv_Differentiation verifies that two calls with different
// sensitive key values produce different scrubbed outputs (not hardcoded).
//
// Specifically: a non-sensitive key added between calls must appear in the
// second output but not the first — proving the scrub reflects live env state.
//
// Traces to: quizzical-marinating-frog.md — Wave V2.G stage 3, item 6 (Rank-8)
func TestScrubGatewayEnv_Differentiation(t *testing.T) {
	const uniqueKey = "OMNIPUS_SCRUB_DIFFERENTIATION_KEY_XYZ"
	t.Setenv(uniqueKey, "")
	os.Unsetenv(uniqueKey)

	// First call: unique key absent.
	scrubbed1 := ScrubGatewayEnv()

	// Set the unique key.
	t.Setenv(uniqueKey, "present-in-second-call")

	// Second call: unique key present.
	scrubbed2 := ScrubGatewayEnv()

	hasKey := func(env []string, key string) bool {
		for _, kv := range env {
			eq := strings.IndexByte(kv, '=')
			if eq > 0 && kv[:eq] == key {
				return true
			}
		}
		return false
	}

	if hasKey(scrubbed1, uniqueKey) {
		t.Errorf("differentiation: unique key must be absent in first call (was not set)")
	}
	if !hasKey(scrubbed2, uniqueKey) {
		t.Errorf("differentiation: unique key must be present in second call (was set to non-empty)")
	}
	// This proves ScrubGatewayEnv reads live env state, not a cached snapshot.
	if len(scrubbed1) == len(scrubbed2) && len(scrubbed2) > 0 {
		// Length may differ by exactly 1 (the added unique key).
		// We already checked presence — if they were identical the above checks would have caught it.
		_ = scrubbed1
	}
}
