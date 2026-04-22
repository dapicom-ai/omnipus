//go:build !cgo

// Package gateway — test-only exports.
//
// This file exposes unexported helpers to package-level tests so that tests
// can call real production code paths rather than re-implementing them.
// Do NOT call these from production code.

package gateway

import (
	"sync"
	"time"
)

// ResetLandlockABIv4WarnForTests resets the sync.Once that gates the
// Sprint-K k07 one-shot ABI-v4 boot warning so tests can exercise the
// emission path repeatedly in the same process.
func ResetLandlockABIv4WarnForTests() {
	landlockABIv4WarnOnce = sync.Once{}
}

// WarnLandlockABIv4OnceForTests exposes warnLandlockABIv4Once to
// in-package tests under an explicit test-only name. Production code
// calls warnLandlockABIv4Once directly from applySandbox.
func WarnLandlockABIv4OnceForTests(abiVersion int) {
	warnLandlockABIv4Once(abiVersion)
}

// ClearDegradedForTest calls the real clearDegraded closure logic on the
// services struct. It is used by reload rollback tests to verify that the
// degraded fields are zeroed without spinning up the full service graph.
func (s *services) ClearDegradedForTest() {
	s.reloadMu.Lock()
	s.reloadDegraded = false
	s.reloadError = nil
	s.reloadMu.Unlock()
}

// SetOrphanWatchdogTimeoutForTest overrides orphanWatchdogTimeout for the duration
// of a test, restoring the original value at test cleanup.
// Used by TestSpawn_OrphanSubTurn_EmitsInterruptedAfter5s to avoid sleeping 5 seconds.
func SetOrphanWatchdogTimeoutForTest(d time.Duration) func() {
	orig := orphanWatchdogTimeout
	orphanWatchdogTimeout = d
	return func() { orphanWatchdogTimeout = orig }
}
