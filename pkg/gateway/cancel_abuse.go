//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/audit"
)

// cancelAbuseDetector tracks cancel-request rates per (user, channel) pair and
// emits a cancel.abuse_pattern audit event when a burst threshold is crossed
// within a sliding time window (FR-25a).
type cancelAbuseDetector struct {
	mu      sync.Mutex
	windows map[string][]time.Time // key: user + "|" + channel
	window  time.Duration          // sliding window size (default 60s)
	burstAt int                    // burst threshold (default 10)
}

func newCancelAbuseDetector() *cancelAbuseDetector {
	return &cancelAbuseDetector{
		windows: make(map[string][]time.Time),
		window:  60 * time.Second,
		burstAt: 10,
	}
}

// recordAttempt records a cancel attempt for the given (user, channel) pair.
// If the number of attempts within the sliding window reaches burstAt, a single
// cancel.abuse_pattern WARNING audit entry is emitted and the window is reset so
// repeated bursts each generate exactly one event (not a flood).
//
// auditLogger may be nil (audit disabled); the emit is a no-op in that case.
func (d *cancelAbuseDetector) recordAttempt(ctx context.Context, user, channel string, at time.Time, auditLogger *audit.Logger) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := user + "|" + channel
	cutoff := at.Add(-d.window)

	// Prune timestamps older than the window.
	entries := d.windows[key]
	filtered := entries[:0]
	for _, t := range entries {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	filtered = append(filtered, at)
	d.windows[key] = filtered

	// Emit exactly one warning per burst crossing.
	if len(filtered) >= d.burstAt {
		count := len(filtered)
		windowSecs := d.window.Seconds()
		// Reset so the next burst generates a fresh event rather than flooding.
		d.windows[key] = nil

		audit.Emit(ctx, auditLogger, audit.EventCancelAbusePattern, audit.SeverityWarn, map[string]any{
			"canceller_user":     user,
			"canceller_channel":  channel,
			"attempts_in_window": count,
			"window_seconds":     windowSecs,
		})
	}
}
