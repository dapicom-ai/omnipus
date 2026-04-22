//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/session"
)

// retentionSweepMu serialises nightly-sweep ticks against the on-demand
// POST /api/v1/security/retention/sweep endpoint. Both paths acquire
// this mutex before calling the sweep function so they never run concurrently.
var retentionSweepMu sync.Mutex

// retentionSweepFn is the function called on each enabled tick. The default
// delegates directly to (*session.UnifiedStore).RetentionSweep. Tests replace
// this variable with a mock to observe call counts without touching the
// filesystem.
var retentionSweepFn func(store *session.UnifiedStore, days int) (int, error) = func(store *session.UnifiedStore, days int) (int, error) {
	return store.RetentionSweep(days)
}

// retentionLoopStarted ensures the goroutine is launched at most once per
// gateway process (sync.Once is reset only at process exit, which is correct
// for a singleton worker).
var retentionLoopStarted sync.Once

// startRetentionSweepLoop launches the nightly retention sweep goroutine.
//
// The goroutine is guarded by retentionLoopStarted so it is launched exactly
// once per process even if the caller is invoked more than once (e.g. during
// integration tests that call the gateway multiple times).
//
// Parameters:
//   - ctx: cancelled by gateway shutdown; the goroutine exits within the next
//     ticker interval (at most tickInterval) after cancellation.
//   - store: the shared UnifiedStore whose sessions are swept.
//   - getCfg: thunk that returns the current config on each call; the goroutine
//     re-evaluates it on every tick so hot-reload changes to retention config
//     are picked up without a restart.
//   - tickInterval: normally 24*time.Hour; pass a smaller value in tests.
func startRetentionSweepLoop(
	ctx context.Context,
	store *session.UnifiedStore,
	getCfg func() *config.Config,
	tickInterval time.Duration,
) {
	retentionLoopStarted.Do(func() {
		go runRetentionSweepLoop(ctx, store, getCfg, tickInterval)
	})
}

func runRetentionSweepLoop(
	ctx context.Context,
	store *session.UnifiedStore,
	getCfg func() *config.Config,
	tickInterval time.Duration,
) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			executeSweepTick(store, getCfg)
		}
	}
}

func executeSweepTick(store *session.UnifiedStore, getCfg func() *config.Config) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("retention_sweep: tick panic recovered",
				"event", "retention_sweep_panic",
				"panic", r,
			)
		}
	}()

	cfg := getCfg()
	if cfg == nil {
		slog.Warn("retention_sweep: getCfg returned nil, skipping tick")
		return
	}

	ret := cfg.Storage.Retention
	if ret.IsDisabled() {
		slog.Info("retention_sweep: skipping tick",
			"event", "retention_sweep_skipped",
			"reason", "disabled",
		)
		return
	}

	days := ret.RetentionSessionDays()

	retentionSweepMu.Lock()
	defer retentionSweepMu.Unlock()

	start := time.Now()
	removed, err := retentionSweepFn(store, days)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		slog.Error("retention_sweep: sweep failed",
			"event", "retention_sweep_failed",
			"error", err,
			"duration_ms", durationMs,
		)
		return
	}

	slog.Info("retention_sweep: completed",
		"event", "retention_sweep",
		"removed", removed,
		"duration_ms", durationMs,
	)
}
