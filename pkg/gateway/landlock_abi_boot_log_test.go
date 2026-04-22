//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/sandbox"
)

// TestLandlockABIv4_BootLogOnce verifies:
//   - abi_version=4 triggers exactly one slog.Warn with event=landlock_abi_warning
//   - a second invocation of the warn site does NOT produce a second entry
//     (the sync.Once guard holds for the lifetime of the gateway process)
func TestLandlockABIv4_BootLogOnce(t *testing.T) {
	// Replace the default slog logger with one that writes JSON to a
	// buffer so we can parse entries back out. Restore at test end.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Reset the once so a prior test in this package does not starve us
	// of the emission. Also ensure we leave the process in a sane state
	// for subsequent tests.
	landlockABIv4WarnOnce = sync.Once{}
	t.Cleanup(func() { landlockABIv4WarnOnce = sync.Once{} })

	// First invocation: must emit one entry.
	warnLandlockABIv4Once(4)

	countWarnings := func() int {
		n := 0
		for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
			if line == "" {
				continue
			}
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("parse log line %q: %v", line, err)
			}
			if entry["event"] == "landlock_abi_warning" {
				n++
			}
		}
		return n
	}

	if got := countWarnings(); got != 1 {
		t.Fatalf("after first warn call: got %d landlock_abi_warning entries, want 1\nlog:\n%s",
			got, buf.String())
	}

	// Verify the required fields are populated on that single entry.
	// Walk the lines again, parse the first matching entry, and assert
	// on its content.
	var entry map[string]any
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("parse log line %q: %v", line, err)
		}
		if e["event"] == "landlock_abi_warning" {
			entry = e
			break
		}
	}
	if entry == nil {
		t.Fatal("expected one landlock_abi_warning entry; found none")
	}
	if got, want := entry["issue"], sandbox.LandlockABI4IssueRef; got != want {
		t.Errorf("issue field: got %v, want %v", got, want)
	}
	if got, want := entry["abi_version"], float64(4); got != want {
		t.Errorf("abi_version field: got %v, want %v", got, want)
	}
	// msg should contain the issue ref inline so operators scanning raw
	// logs see the tracker reference even without structured parsing.
	if msg, _ := entry["msg"].(string); !strings.Contains(msg, sandbox.LandlockABI4IssueRef) {
		t.Errorf("msg must contain %q; got %q", sandbox.LandlockABI4IssueRef, msg)
	}

	// Second invocation: must NOT emit a second entry. sync.Once gates.
	warnLandlockABIv4Once(4)
	if got := countWarnings(); got != 1 {
		t.Fatalf("after second warn call: got %d landlock_abi_warning entries, want 1\nlog:\n%s",
			got, buf.String())
	}

	// Third invocation with an even higher abi (e.g. v5) also does not
	// produce a new entry — once tripped, always silent.
	warnLandlockABIv4Once(5)
	if got := countWarnings(); got != 1 {
		t.Fatalf("after third warn call: got %d landlock_abi_warning entries, want 1\nlog:\n%s",
			got, buf.String())
	}
}

// TestLandlockABIv4_BelowThresholdNoLog verifies that abi_version < 4
// produces no entry at all — the warning is strictly for kernels that
// outrank our negotiation.
func TestLandlockABIv4_BelowThresholdNoLog(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	landlockABIv4WarnOnce = sync.Once{}
	t.Cleanup(func() { landlockABIv4WarnOnce = sync.Once{} })

	warnLandlockABIv4Once(3)
	warnLandlockABIv4Once(0)

	if strings.Contains(buf.String(), "landlock_abi_warning") {
		t.Fatalf("no warn expected for abi<4; got:\n%s", buf.String())
	}
}
