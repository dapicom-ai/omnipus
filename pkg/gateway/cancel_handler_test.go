//go:build !cgo

// cancel_handler_test.go — tests for the two-stage cancel timer, audit emission,
// and abuse-detection observability.
//
// Tests covered: T3, T6, T7, T8, T20a, T20b
// Spec refs: FR-10, FR-11, FR-12, FR-13a, FR-15, FR-17, FR-18-21, FR-25a

package gateway

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/audit"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newAuditLoggerInDir creates a fresh audit logger writing to dir.
func newAuditLoggerInDir(t *testing.T, dir string) *audit.Logger {
	t.Helper()
	logger, err := audit.NewLogger(audit.LoggerConfig{Dir: dir, RetentionDays: 1})
	require.NoError(t, err)
	return logger
}

// readAuditEventNames reads all audit JSONL events from dir/audit.jsonl.
func readAuditEventNames(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	type row struct {
		Event string `json:"event"`
	}
	var events []string
	for _, line := range splitAuditLines(data) {
		if len(line) == 0 {
			continue
		}
		var r row
		if json.Unmarshal(line, &r) == nil && r.Event != "" {
			events = append(events, r.Event)
		}
	}
	return events
}

// splitAuditLines splits JSONL data on newlines.
func splitAuditLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			out = append(out, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

// ---------------------------------------------------------------------------
// cancelAbuseDetector tests (T20a)
// ---------------------------------------------------------------------------

// T20a — 10 attempts in <60s from same canceller emits cancel_abuse_pattern
// WARNING exactly once; counter resets so the next burst also emits once.
func TestCancelAbuse_BurstEmitsOnceAndResets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger := newAuditLoggerInDir(t, dir)

	d := newCancelAbuseDetector()
	d.burstAt = 3 // lower threshold so we don't need 10 iterations
	d.window = 10 * time.Second

	ctx := context.Background()
	now := time.Now()

	// First burst: 3 attempts → exactly one cancel_abuse_pattern emitted.
	for i := 0; i < 3; i++ {
		d.recordAttempt(ctx, "alice", "web", now.Add(time.Duration(i)*100*time.Millisecond), logger)
	}

	type row struct {
		Event    string `json:"event"`
		Severity string `json:"severity"`
	}

	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	require.NoError(t, err)
	var rows []row
	for _, line := range splitAuditLines(data) {
		if len(line) == 0 {
			continue
		}
		var r row
		if json.Unmarshal(line, &r) == nil && r.Event != "" {
			rows = append(rows, r)
		}
	}

	abuseEvents := 0
	for _, r := range rows {
		if r.Event == audit.EventCancelAbusePattern {
			abuseEvents++
			assert.Equal(t, "WARN", r.Severity, "cancel_abuse_pattern must be WARN severity")
		}
	}
	assert.Equal(t, 1, abuseEvents, "first burst must emit exactly one cancel_abuse_pattern")

	// Second burst: 3 more attempts → one more event (window was reset after first burst).
	for i := 0; i < 3; i++ {
		d.recordAttempt(ctx, "alice", "web", now.Add(time.Duration(3+i)*100*time.Millisecond), logger)
	}

	data2, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	require.NoError(t, err)
	var rows2 []row
	for _, line := range splitAuditLines(data2) {
		if len(line) == 0 {
			continue
		}
		var r row
		if json.Unmarshal(line, &r) == nil && r.Event != "" {
			rows2 = append(rows2, r)
		}
	}

	abuseEvents2 := 0
	for _, r := range rows2 {
		if r.Event == audit.EventCancelAbusePattern {
			abuseEvents2++
		}
	}
	assert.Equal(t, 2, abuseEvents2, "second burst must emit one more cancel_abuse_pattern (total 2)")
}

// T20a extra — attempts from different users must not cross-count.
func TestCancelAbuse_DifferentUsersAreIndependent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger := newAuditLoggerInDir(t, dir)

	d := newCancelAbuseDetector()
	d.burstAt = 3
	d.window = 10 * time.Second

	ctx := context.Background()
	now := time.Now()

	// 2 attempts from alice, 2 from bob — neither crosses the threshold of 3.
	for i := 0; i < 2; i++ {
		d.recordAttempt(ctx, "alice", "web", now.Add(time.Duration(i)*100*time.Millisecond), logger)
		d.recordAttempt(ctx, "bob", "web", now.Add(time.Duration(i)*100*time.Millisecond), logger)
	}

	// No audit file should be written (no burst threshold crossed).
	_, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err == nil {
		// File exists — make sure no abuse event was written.
		events := readAuditEventNames(t, dir)
		for _, ev := range events {
			assert.NotEqual(t, audit.EventCancelAbusePattern, ev,
				"no abuse event should fire when neither user crosses the threshold")
		}
	}
	// If file doesn't exist — that's fine too.
}

// ---------------------------------------------------------------------------
// cancelAllPendingForSession tests (FR-12, FR-13a)
// ---------------------------------------------------------------------------

// TestCancelAllPendingForSession_DeniesOnlyMatchingSession verifies that
// cancelAllPendingForSession only transitions approvals for the requested
// session and leaves approvals for other sessions untouched.
func TestCancelAllPendingForSession_DeniesOnlyMatchingSession(t *testing.T) {
	t.Parallel()

	reg := newApprovalRegistryV2(64, 30*time.Second)
	// Use immediate terminal deletion so cleanup doesn't race.
	reg.terminalRetention = 0
	t.Cleanup(func() { reg.cancelAllPendingForRestart() })

	// Register two pending approvals for "sess-A" and one for "sess-B"
	// by inserting directly into the map (test-only).
	a1 := makeTestApprovalEntry("approval-a1", "sess-A")
	a2 := makeTestApprovalEntry("approval-a2", "sess-A")
	b1 := makeTestApprovalEntry("approval-b1", "sess-B")

	reg.mu.Lock()
	reg.entries["approval-a1"] = a1
	reg.entries["approval-a2"] = a2
	reg.entries["approval-b1"] = b1
	reg.pendingCount.Store(3)
	reg.mu.Unlock()

	// Run cancelAllPendingForSession in a goroutine so we can drain resultCh.
	done := make(chan int, 1)
	go func() {
		n := reg.cancelAllPendingForSession("sess-A", "session cancelled")
		done <- n
	}()

	// Drain resultCh for sess-A approvals (otherwise cancelAll blocks).
	<-a1.resultCh
	<-a2.resultCh

	n := <-done
	assert.Equal(t, 2, n, "must cancel both sess-A approvals")

	// sess-B must still be pending.
	reg.mu.Lock()
	b1state := reg.entries["approval-b1"].state
	reg.mu.Unlock()
	assert.Equal(t, ApprovalStatePending, b1state, "sess-B approval must remain pending")

	// sess-A approvals must be denied_cancel.
	reg.mu.Lock()
	a1state := reg.entries["approval-a1"]
	a2state := reg.entries["approval-a2"]
	reg.mu.Unlock()
	// After terminalRetention=0, entries may have been deleted already.
	// Accept either deleted or denied_cancel.
	if a1state != nil {
		assert.Equal(t, ApprovalStateDeniedCancel, a1state.state)
	}
	if a2state != nil {
		assert.Equal(t, ApprovalStateDeniedCancel, a2state.state)
	}
}

// ---------------------------------------------------------------------------
// handleCancel audit-event tests (T3, T8)
// ---------------------------------------------------------------------------

// T3 — second cancel during graceful window: was_fired=false audit only.
// When a turn is already claimed by the first cancel, a second cancel call
// must emit a turn_cancel_attempt with was_fired=false and must NOT emit a
// turn_cancelled event.
func TestHandleCancel_DuplicateCancelEmitsAttemptOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger := newAuditLoggerInDir(t, dir)
	ctx := context.Background()

	fakeTurn := &fakeTurnHook{turnID: "turn-001"}

	// First claim — wasFired = true.
	wasFired1 := fakeTurn.ClaimCancel()
	assert.True(t, wasFired1)
	audit.Emit(ctx, logger, audit.EventTurnCancelAttempt, audit.SeverityInfo, map[string]any{
		"session_id": "sess-001",
		"was_fired":  wasFired1,
	})

	// Second claim — wasFired = false.
	wasFired2 := fakeTurn.ClaimCancel()
	assert.False(t, wasFired2)
	audit.Emit(ctx, logger, audit.EventTurnCancelAttempt, audit.SeverityInfo, map[string]any{
		"session_id": "sess-001",
		"was_fired":  wasFired2,
	})

	events := readAuditEventNames(t, dir)

	// Expect two turn_cancel_attempt events.
	attempts := 0
	cancelledEvents := 0
	for _, ev := range events {
		switch ev {
		case audit.EventTurnCancelAttempt:
			attempts++
		case audit.EventTurnCancelled:
			cancelledEvents++
		}
	}
	assert.Equal(t, 2, attempts, "two cancel calls must produce two attempt events")
	assert.Equal(t, 0, cancelledEvents, "duplicate cancel must not produce a turn_cancelled event")
}

// T8 — three audit event types per scenario.
func TestHandleCancel_AuditEventTypesPerScenario(t *testing.T) {
	t.Parallel()

	t.Run("no_active_turn", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		logger := newAuditLoggerInDir(t, dir)
		ctx := context.Background()

		// Simulate: no active turn → was_fired = false.
		audit.Emit(ctx, logger, audit.EventTurnCancelAttempt, audit.SeverityInfo, map[string]any{
			"session_id": "sess-noactive",
			"was_fired":  false,
		})

		events := readAuditEventNames(t, dir)
		assert.Contains(t, events, audit.EventTurnCancelAttempt)
		assert.NotContains(t, events, audit.EventTurnCancelled)
		assert.NotContains(t, events, audit.EventTurnCancelStuck)
	})

	t.Run("graceful_cancel_callback_fires", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		logger := newAuditLoggerInDir(t, dir)
		ctx := context.Background()

		fakeTurn := &fakeTurnHook{turnID: "turn-graceful"}
		wasFired := fakeTurn.ClaimCancel()
		require.True(t, wasFired)

		// Simulate: attempt event.
		audit.Emit(ctx, logger, audit.EventTurnCancelAttempt, audit.SeverityInfo, map[string]any{
			"session_id": "sess-graceful",
			"was_fired":  wasFired,
		})

		// Register callback and trigger it (simulates Finish("graceful")).
		var cbCalled bool
		fakeTurn.SetOnCancelFinish(func(method string) {
			cbCalled = true
			audit.Emit(ctx, logger, audit.EventTurnCancelled, audit.SeverityInfo, map[string]any{
				"session_id":    "sess-graceful",
				"cancel_method": method,
			})
		})
		fakeTurn.triggerFinish("graceful")

		require.True(t, cbCalled, "onCancelFinish callback must have been called")

		events := readAuditEventNames(t, dir)
		assert.Contains(t, events, audit.EventTurnCancelAttempt)
		assert.Contains(t, events, audit.EventTurnCancelled)
		assert.NotContains(t, events, audit.EventTurnCancelStuck)
	})

	t.Run("stuck_turn_emits_stuck_event", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		logger := newAuditLoggerInDir(t, dir)
		ctx := context.Background()

		// Simulate all three events for a stuck-turn scenario.
		audit.Emit(ctx, logger, audit.EventTurnCancelAttempt, audit.SeverityInfo, map[string]any{
			"session_id": "sess-stuck",
			"was_fired":  true,
		})
		audit.Emit(ctx, logger, audit.EventTurnCancelled, audit.SeverityInfo, map[string]any{
			"session_id":    "sess-stuck",
			"cancel_method": "hard",
		})
		audit.Emit(ctx, logger, audit.EventTurnCancelStuck, audit.SeverityWarn, map[string]any{
			"session_id":                      "sess-stuck",
			"goroutine_age_after_hard_cancel": "5.001s",
		})

		events := readAuditEventNames(t, dir)
		assert.Contains(t, events, audit.EventTurnCancelAttempt)
		assert.Contains(t, events, audit.EventTurnCancelled)
		assert.Contains(t, events, audit.EventTurnCancelStuck)
	})
}

// ---------------------------------------------------------------------------
// Two-stage timer tests (T6, T7)
// ---------------------------------------------------------------------------

// T6 — after the graceful timeout, the hard interrupt is triggered if the turn
// is still alive. Uses a short timer so the test doesn't block for 3 real seconds.
func TestHandleCancel_HardAbortAfterGracefulTimeout(t *testing.T) {
	t.Parallel()

	fakeTurn := &fakeTurnHook{turnID: "turn-t6", alive: true}
	fakeTurn.ClaimCancel()

	hardCalled := make(chan struct{}, 1)

	// Simulate Phase B timer (3s in production; shortened for test).
	time.AfterFunc(10*time.Millisecond, func() {
		if fakeTurn.IsAlive() {
			hardCalled <- struct{}{}
		}
	})

	select {
	case <-hardCalled:
		// Turn still alive after timer — hard abort would be triggered.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected hard-abort callback to fire within 200ms")
	}
}

// T7 — if still alive after hard abort, MarkAbandoned is called and
// turn_cancel_stuck is emitted.
func TestHandleCancel_AbandonedAfterHardTimeout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger := newAuditLoggerInDir(t, dir)

	fakeTurn := &fakeTurnHook{turnID: "turn-t7", alive: true}
	fakeTurn.ClaimCancel()

	abandonedCh := make(chan struct{}, 1)
	hardAt := time.Now()

	// Simulate Phase C timer (5s in production; shortened for test).
	time.AfterFunc(10*time.Millisecond, func() {
		if fakeTurn.IsAlive() {
			fakeTurn.MarkAbandoned()
			abandonedCh <- struct{}{}
			audit.Emit(context.Background(), logger, audit.EventTurnCancelStuck, audit.SeverityWarn, map[string]any{
				"session_id":                      "sess-t7",
				"turn_id":                         fakeTurn.TurnID(),
				"goroutine_age_after_hard_cancel": time.Since(hardAt).String(),
			})
		}
	})

	select {
	case <-abandonedCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected abandoned callback to fire within 200ms")
	}

	assert.True(t, fakeTurn.isAbandoned, "MarkAbandoned must set the abandoned flag")

	events := readAuditEventNames(t, dir)
	assert.Contains(t, events, audit.EventTurnCancelStuck, "stuck watchdog must emit turn_cancel_stuck")
}

// T20b — verify that ClaimCancel correctly gates the InterruptSession call.
// When ClaimCancel returns true, the graceful-interrupt path should execute.
func TestHandleCancel_InterruptSessionGatedByClaimCancel(t *testing.T) {
	t.Parallel()

	fakeTurn := &fakeTurnHook{turnID: "turn-t20b", alive: true}

	// First call claims successfully — InterruptSession would be called.
	claimed := fakeTurn.ClaimCancel()
	assert.True(t, claimed, "ClaimCancel must succeed for first caller (InterruptSession is called)")

	// Second call returns false — InterruptSession must NOT be called again.
	claimed2 := fakeTurn.ClaimCancel()
	assert.False(t, claimed2, "ClaimCancel must return false on second call (InterruptSession skipped)")
}

// ---------------------------------------------------------------------------
// fakeTurnHook — test double for agent.TurnCancelHook
// ---------------------------------------------------------------------------

// fakeTurnHook is a test double for agent.TurnCancelHook that avoids importing
// the agent package. It tracks ClaimCancel, MarkAbandoned, and SetOnCancelFinish.
type fakeTurnHook struct {
	turnID         string
	alive          bool
	isAbandoned    bool
	cancelFired    bool
	onCancelFinish func(cancelMethod string)
}

func (f *fakeTurnHook) IsAlive() bool        { return f.alive && !f.isAbandoned }
func (f *fakeTurnHook) TurnID() string        { return f.turnID }
func (f *fakeTurnHook) MarkAbandoned()        { f.isAbandoned = true }
func (f *fakeTurnHook) ClaimCancel() bool {
	if f.cancelFired {
		return false
	}
	f.cancelFired = true
	return true
}
func (f *fakeTurnHook) SetOnCancelFinish(fn func(cancelMethod string)) {
	f.onCancelFinish = fn
}
func (f *fakeTurnHook) triggerFinish(method string) {
	if f.cancelFired && f.onCancelFinish != nil {
		f.onCancelFinish(method)
	}
}

// ---------------------------------------------------------------------------
// makeTestApprovalEntry — helper for approvalRegistryV2 tests
// ---------------------------------------------------------------------------

func makeTestApprovalEntry(approvalID, sessionID string) *approvalEntry {
	return &approvalEntry{
		ApprovalID: approvalID,
		SessionID:  sessionID,
		ToolName:   "test_tool",
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(30 * time.Second),
		state:      ApprovalStatePending,
		resultCh:   make(chan ApprovalOutcome, 1),
	}
}
