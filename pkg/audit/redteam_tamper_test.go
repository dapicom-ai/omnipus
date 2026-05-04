// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Package audit_test — insider-LLM red-team coverage for audit-log tampering.
//
// This file documents threat C2-AUDIT (audit log tampering) from the
// insider-pentest report. The threat: an attacker (or a compromised plugin)
// who obtains write access to ~/.omnipus/system/audit.jsonl can:
//
//  1. Truncate the log to hide a sequence of malicious tool calls.
//  2. Surgically rewrite a previously-written line (e.g. flip
//     decision: "deny" -> "allow") to retroactively launder a denied call.
//
// The defense requires per-entry HMAC chaining: each entry carries
// hmac(prev_hash || marshaled_entry) under a key the gateway holds and the
// log file does NOT contain. A standalone integrity verifier then walks the
// log start-to-end, recomputes each link, and reports the first broken link.
//
// Closing fix: v0.2 (#155) — "audit log integrity (HMAC chain)".
//
// These tests will FAIL today because:
//   - audit.Entry has no Hash / PrevHash field.
//   - No `audit.VerifyChain(path) (BrokenLink, error)` exists.
// The test refers to the API the fix is expected to expose; once the fix
// lands and the API matches, the test compiles and passes.
//
// To keep this file BUILDABLE today (so the suite still runs and surfaces
// other failures), we drive the test through reflection-style probing of
// the public package surface: if the verifier function is absent we report
// the gap as a t.Errorf; if it is present we call it and assert it flags
// the tamper. Either way the test never silently passes.
package audit_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/audit"
)

// TestRedteam_AuditLog_TruncationDetected documents the truncation half of
// C2-AUDIT. It writes three audit entries through the production logger,
// closes the logger, then truncates the file by removing the last entry.
// A correct integrity verifier MUST report "chain broken" — the truncated
// file's tail no longer matches the recorded chain length / final HMAC.
//
// Today: no verifier exists. Test FAILS.
func TestRedteam_AuditLog_TruncationDetected(t *testing.T) {
	t.Logf("documents C2-AUDIT (audit truncation) from insider-pentest report; closes when v0.2 #155 HMAC chain lands")

	dir := t.TempDir()
	logger, err := audit.NewLogger(audit.LoggerConfig{
		Dir:           dir,
		RetentionDays: 90,
	})
	require.NoError(t, err)

	// Write three sequential entries — these would form a 3-link chain in the
	// fixed implementation. Each carries a distinct decision so a tamper
	// against any one of them is observable.
	for i, decision := range []string{audit.DecisionAllow, audit.DecisionDeny, audit.DecisionAllow} {
		entry := &audit.Entry{
			Timestamp:  time.Now().UTC().Add(time.Duration(i) * time.Millisecond),
			Event:      audit.EventToolCall,
			Decision:   decision,
			AgentID:    "redteam-agent",
			SessionID:  "sess-truncate",
			Tool:       "shell",
			PolicyRule: "redteam: forced for tamper test",
			Details:    map[string]any{"seq": i},
		}
		require.NoError(t, logger.Log(entry), "seed entry %d", i)
	}
	require.NoError(t, logger.Close())

	auditPath := filepath.Join(dir, "audit.jsonl")
	original, err := os.ReadFile(auditPath)
	require.NoError(t, err)

	// Drop the LAST line — simulates an attacker who deleted the most recent
	// entry to hide a denied call. We trim back to the byte position of the
	// final newline so the file remains valid JSONL syntactically; the
	// tampering is only detectable via chain integrity.
	truncated := dropLastLine(original)
	require.NoError(t, os.WriteFile(auditPath, truncated, 0o600))

	// Probe: does an integrity verifier exist on the audit package?
	if !auditPackageHasIntegrityVerifier() {
		t.Errorf(
			"C2-AUDIT (truncation) GAP CONFIRMED: no audit chain verifier exists (expected audit.VerifyChain or equivalent). "+
				"Truncation of audit.jsonl from %d to %d bytes (1 entry removed) is currently UNDETECTED. "+
				"Fix: ship per-entry HMAC chain in v0.2 (#155).",
			len(original), len(truncated),
		)
		return
	}

	// If a verifier API has been added, exercise it. The test will need a
	// real type-safe call when the fix lands; for now this branch is dead
	// code, but the test will compile against the future API.
	verifyFn, ok := lookupAuditVerifier()
	if !ok {
		t.Fatalf("integrity verifier reported present but lookup failed — recompile audit package")
	}
	res := verifyFn(auditPath)
	if res == nil || res.IsValid() {
		t.Errorf("C2-AUDIT (truncation): verifier reported the chain VALID after truncating one entry — fix is broken")
	}
}

// TestRedteam_AuditLog_RewriteDetected documents the surgical-rewrite half of
// C2-AUDIT. We write three entries (allow, deny, allow), then SURGICALLY
// edit the middle entry's decision in-place from "deny" to "allow" without
// changing the byte length. With a correct HMAC chain, this MUST flag the
// chain as broken at link #2 (the rewritten entry would compute a different
// hash than what is referenced by link #3).
//
// Today: no verifier exists. Test FAILS.
func TestRedteam_AuditLog_RewriteDetected(t *testing.T) {
	t.Logf("documents C2-AUDIT (audit rewrite) from insider-pentest report; closes when v0.2 #155 HMAC chain lands")

	dir := t.TempDir()
	logger, err := audit.NewLogger(audit.LoggerConfig{
		Dir:           dir,
		RetentionDays: 90,
	})
	require.NoError(t, err)

	entries := []*audit.Entry{
		{
			Timestamp: time.Now().UTC(),
			Event:     audit.EventToolCall,
			Decision:  audit.DecisionAllow,
			AgentID:   "redteam-agent",
			SessionID: "sess-rewrite",
			Tool:      "echo",
		},
		{
			Timestamp:  time.Now().UTC().Add(1 * time.Millisecond),
			Event:      audit.EventToolCall,
			Decision:   audit.DecisionDeny,
			AgentID:    "redteam-agent",
			SessionID:  "sess-rewrite",
			Tool:       "shell",
			PolicyRule: "shellguard: dangerous pattern",
		},
		{
			Timestamp: time.Now().UTC().Add(2 * time.Millisecond),
			Event:     audit.EventToolCall,
			Decision:  audit.DecisionAllow,
			AgentID:   "redteam-agent",
			SessionID: "sess-rewrite",
			Tool:      "ls",
		},
	}
	for i, e := range entries {
		require.NoError(t, logger.Log(e), "seed entry %d", i)
	}
	require.NoError(t, logger.Close())

	auditPath := filepath.Join(dir, "audit.jsonl")

	// Surgical rewrite: parse JSONL line-by-line, locate the entry we want
	// to launder (the deny in slot #1), flip decision from "deny" to "allow"
	// while preserving the byte-length wherever possible by padding. The
	// HMAC chain doesn't care about byte length — it cares about each
	// entry's content hash — so the rewrite is detectable as long as the
	// payload changes at all.
	require.NoError(t, rewriteDecision(auditPath, 1, audit.DecisionDeny, audit.DecisionAllow))

	if !auditPackageHasIntegrityVerifier() {
		t.Errorf(
			"C2-AUDIT (rewrite) GAP CONFIRMED: no audit chain verifier exists. " +
				"In-place rewrite of audit.jsonl entry #1 (decision: deny -> allow) is currently UNDETECTED. " +
				"Fix: ship per-entry HMAC chain in v0.2 (#155).",
		)
		return
	}

	verifyFn, ok := lookupAuditVerifier()
	if !ok {
		t.Fatalf("integrity verifier reported present but lookup failed — recompile audit package")
	}
	res := verifyFn(auditPath)
	if res == nil || res.IsValid() {
		t.Errorf("C2-AUDIT (rewrite): verifier reported the chain VALID after rewriting entry #1 — fix is broken")
	}
}

// dropLastLine returns the input with the trailing newline-terminated line
// removed. If the input contains zero or one newlines, returns the input
// unchanged so the caller can detect "no work to do".
func dropLastLine(in []byte) []byte {
	// Trim trailing newline first so the search hits the line BEFORE last.
	end := len(in)
	if end == 0 {
		return in
	}
	if in[end-1] == '\n' {
		end--
	}
	// Find the last newline before `end`.
	for i := end - 1; i >= 0; i-- {
		if in[i] == '\n' {
			return in[:i+1]
		}
	}
	// Only one line: empty file is the "everything dropped" outcome.
	return nil
}

// rewriteDecision opens an audit.jsonl, locates the lineIdx'th line, flips
// the JSON `decision` field from `from` to `to`, and rewrites the file in
// place. Other fields are preserved exactly. Returns an error if the line
// index is out of range or the decision field doesn't match `from`.
func rewriteDecision(path string, lineIdx int, from, to string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := splitJSONLines(data)
	if lineIdx < 0 || lineIdx >= len(lines) {
		return os.ErrInvalid
	}

	var entry map[string]any
	if err := json.Unmarshal(lines[lineIdx], &entry); err != nil {
		return err
	}
	if got, _ := entry["decision"].(string); got != from {
		return os.ErrInvalid
	}
	entry["decision"] = to

	rewritten, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	lines[lineIdx] = rewritten

	// Reassemble.
	var out []byte
	for _, l := range lines {
		out = append(out, l...)
		out = append(out, '\n')
	}
	return os.WriteFile(path, out, 0o600)
}

// splitJSONLines splits a JSONL byte slice into per-line slices, dropping
// trailing empty fragments.
func splitJSONLines(in []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range in {
		if b == '\n' {
			if i > start {
				line := make([]byte, i-start)
				copy(line, in[start:i])
				out = append(out, line)
			}
			start = i + 1
		}
	}
	if start < len(in) {
		line := make([]byte, len(in)-start)
		copy(line, in[start:])
		out = append(out, line)
	}
	return out
}

// chainVerificationResult is the contract the future verifier API is
// expected to return. We declare it locally so this test file compiles
// today — when the real type ships, swap this declaration for the import.
type chainVerificationResult interface {
	IsValid() bool
}

// auditPackageHasIntegrityVerifier reports whether the audit package
// exports a chain-integrity verifier function. Today the package does
// not, so this returns false. Once #155 ships an `audit.VerifyChain` (or
// equivalent) function with the expected signature, this returns true.
//
// We probe via reflection so the test file does not fail to COMPILE just
// because the symbol is absent — keeping it buildable lets the rest of
// the suite run and surface other failures.
func auditPackageHasIntegrityVerifier() bool {
	_, ok := lookupAuditVerifier()
	return ok
}

// lookupAuditVerifier returns the audit verifier function if it has been
// added to the package. Today: not present, returns (nil, false).
//
// When the v0.2 fix exposes `audit.VerifyChain(path string) *audit.ChainResult`
// this function should be replaced with a direct reference and the
// reflection probe deleted. The reflection layer exists so the test file
// keeps compiling against the current package.
func lookupAuditVerifier() (func(string) chainVerificationResult, bool) {
	// Reflection probe: enumerate every public function/var on the audit
	// package — but we don't have direct package reflection in Go. Instead
	// we rely on the fact that any future `VerifyChain` will be wired in
	// here at the moment the fix lands, by editing this single function.
	//
	// For the moment, no such symbol exists and we return (nil, false).
	// The presence of THIS sentinel guarantees the test file will need to
	// be updated when the fix lands — making the gap a compile-time
	// follow-up, not a silent pass.
	var probe interface{} = (*audit.Logger)(nil)
	_ = reflect.TypeOf(probe)
	return nil, false
}
