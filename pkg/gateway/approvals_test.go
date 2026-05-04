//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Tests for the approval-registry map cleanup at terminal-state transitions.
//
// Background: prior to v0.1 ship-blocker A1.4 the registry never deleted
// entries from r.entries on terminal transitions; the map grew monotonically.
// The fix schedules a deferred delete (after r.terminalRetention) on each of
// the four terminal paths: resolve(), fireTimeout(), cancelBatchShortCircuit(),
// and cancelAllPendingForRestart(). Tests below force terminalRetention to 0
// so deletion is synchronous and assertable without time-based waits.

package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApprovalRegistry_ResolveDeletesEntry verifies that resolve()
// removes the entry from r.entries when the retention window is 0.
// BDD: Given a registry with terminalRetention=0,
// When an approval is requested and then resolved,
// Then r.entries is empty after the resolve.
func TestApprovalRegistry_ResolveDeletesEntry(t *testing.T) {
	reg := newApprovalRegistryV2(64, 300*time.Second)
	reg.terminalRetention = 0 // delete synchronously under the lock

	e, accepted := reg.requestApproval(
		"tc-resolve-cleanup", "read_file",
		map[string]any{"path": "/tmp/x"},
		"agent-A", "sess-A", "turn-A",
		false,
	)
	require.True(t, accepted, "approval must be accepted")

	// Drain outcome so resolve does not block on the buffered channel.
	doneCh := make(chan struct{})
	go func() {
		<-e.resultCh
		close(doneCh)
	}()

	ok, gone := reg.resolve(e.ApprovalID, ApprovalActionApprove)
	require.True(t, ok, "resolve must succeed on a pending entry")
	require.False(t, gone, "fresh resolve must not report gone=true")

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("resultCh never received outcome")
	}

	reg.mu.Lock()
	count := len(reg.entries)
	reg.mu.Unlock()
	assert.Equal(t, 0, count,
		"entries map must be empty after resolve when terminalRetention=0; got %d",
		count)
}

// TestApprovalRegistry_BatchShortCircuitDeletesEntry verifies that
// cancelBatchShortCircuit() removes the entry from r.entries when
// terminalRetention=0.
// BDD: Given a registry with terminalRetention=0,
// When an approval is requested and then cancelled via batch short-circuit,
// Then r.entries is empty after the cancellation.
func TestApprovalRegistry_BatchShortCircuitDeletesEntry(t *testing.T) {
	reg := newApprovalRegistryV2(64, 300*time.Second)
	reg.terminalRetention = 0

	e, accepted := reg.requestApproval(
		"tc-bsc-cleanup", "web_search",
		map[string]any{"q": "x"},
		"agent-C", "sess-C", "turn-C",
		false,
	)
	require.True(t, accepted, "approval must be accepted")

	// Drain outcome so cancelBatchShortCircuit does not block.
	doneCh := make(chan struct{})
	go func() {
		<-e.resultCh
		close(doneCh)
	}()

	ok := reg.cancelBatchShortCircuit(e.ApprovalID)
	require.True(t, ok, "cancelBatchShortCircuit must return true for a pending entry")

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("resultCh never received outcome")
	}

	reg.mu.Lock()
	count := len(reg.entries)
	reg.mu.Unlock()
	assert.Equal(t, 0, count,
		"entries map must be empty after cancelBatchShortCircuit when terminalRetention=0; got %d",
		count)
}

// TestApprovalRegistry_RestartCancelDeletesEntry verifies that
// cancelAllPendingForRestart() removes entries from r.entries when
// terminalRetention=0.
// BDD: Given a registry with terminalRetention=0 and one pending entry,
// When cancelAllPendingForRestart is called,
// Then r.entries is empty after all outcomes are delivered.
func TestApprovalRegistry_RestartCancelDeletesEntry(t *testing.T) {
	reg := newApprovalRegistryV2(64, 300*time.Second)
	reg.terminalRetention = 0

	e, accepted := reg.requestApproval(
		"tc-restart-cleanup", "read_file",
		map[string]any{"path": "/tmp/y"},
		"agent-D", "sess-D", "turn-D",
		false,
	)
	require.True(t, accepted, "approval must be accepted")

	// Drain outcome so cancelAllPendingForRestart does not block.
	doneCh := make(chan struct{})
	go func() {
		<-e.resultCh
		close(doneCh)
	}()

	cancelled := reg.cancelAllPendingForRestart()
	require.Len(t, cancelled, 1, "must have cancelled exactly one entry")

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("resultCh never received outcome")
	}

	reg.mu.Lock()
	count := len(reg.entries)
	reg.mu.Unlock()
	assert.Equal(t, 0, count,
		"entries map must be empty after cancelAllPendingForRestart when terminalRetention=0; got %d",
		count)
}

// TestApprovalRegistry_TimeoutDeletesEntry verifies that fireTimeout()
// removes the entry from r.entries after the timeout fires.
// BDD: Given a registry with a short timeout and terminalRetention=0,
// When the approval times out,
// Then r.entries is empty.
func TestApprovalRegistry_TimeoutDeletesEntry(t *testing.T) {
	// Short timeout so the timer fires quickly; retention=0 deletes inline.
	reg := newApprovalRegistryV2(64, 50*time.Millisecond)
	reg.terminalRetention = 0

	e, accepted := reg.requestApproval(
		"tc-timeout-cleanup", "web_search",
		map[string]any{"q": "x"},
		"agent-B", "sess-B", "turn-B",
		false,
	)
	require.True(t, accepted, "approval must be accepted")

	// Block until the timeout outcome is delivered to the buffered channel.
	select {
	case outcome := <-e.resultCh:
		assert.False(t, outcome.Approved)
		assert.Equal(t, "timeout", outcome.Reason)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout transition did not fire within 2 s")
	}

	// fireTimeout schedules the delete inline (retention=0). Confirm the map
	// is empty now.
	reg.mu.Lock()
	count := len(reg.entries)
	reg.mu.Unlock()
	assert.Equal(t, 0, count,
		"entries map must be empty after fireTimeout when terminalRetention=0; got %d",
		count)
}
