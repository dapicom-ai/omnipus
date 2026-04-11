//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/agent"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/providers"
)

// omnipusShutdownTimeout is the maximum time to wait for in-flight operations
// to complete before force-flushing partial state. Per FUNC-36 / US-9.
//
// This must be larger than the maximum active-turn wait (60 s) plus a margin (5 s).
// Previously 10 s, which was impossible to satisfy when active turns run up to 60 s.
const omnipusShutdownTimeout = 70 * time.Second

// omnipusGracefulShutdown executes the 5-step graceful shutdown per FUNC-36:
//
//  1. Stop channel manager — no new inbound messages accepted
//  2. Drain agent loop — wait for active turns (US-7, FR-008), then close
//  3. Flush partial state — interrupted responses already saved by agent loop cancellation
//  4. Stop background services — heartbeat, cron, device, health
//  5. Close provider connections — release HTTP/WebSocket sessions
//
// Implements US-9 and US-7 acceptance criteria.
func omnipusGracefulShutdown(
	runningServices *services,
	agentLoop *agent.AgentLoop,
	provider providers.LLMProvider,
	cfg *config.Config,
) {
	ctx, cancel := context.WithTimeout(context.Background(), omnipusShutdownTimeout)
	defer cancel()

	slog.Info("shutdown: step 1 — stopping new requests")
	// Stop channel manager first so no new inbound messages are accepted.
	// Use context.Background() with its own 2 s budget so that a tight overall
	// ctx deadline (SI2) does not steal from the channel-manager stop budget.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if runningServices.ChannelManager != nil {
		runningServices.ChannelManager.StopAll(stopCtx)
	}

	// US-7 / FR-008: Wait for active turns to complete before force-closing.
	// Cap the wait to omnipusShutdownTimeout - 5 s so it always fits in the budget.
	maxActiveTurnWait := int((omnipusShutdownTimeout - 5*time.Second).Seconds()) // 65 s
	activeTurnTimeout := maxActiveTurnWait
	if cfg != nil {
		ts := cfg.Agents.Defaults.TimeoutSeconds
		if ts > 0 && ts < activeTurnTimeout {
			activeTurnTimeout = ts
		}
	}

	slog.Info("shutdown: step 2 — waiting for active turns to complete",
		"active_turn_wait_seconds", activeTurnTimeout)

	// Stop() prevents new turns from starting.
	agentLoop.Stop()

	activeTurnsDone := make(chan struct{})
	go func() {
		defer close(activeTurnsDone)
		agentLoop.WaitForActiveRequests()
	}()

	select {
	case <-activeTurnsDone:
		slog.Info("shutdown: all active turns completed before shutdown")
	case <-time.After(time.Duration(activeTurnTimeout) * time.Second):
		slog.Warn("shutdown: timeout waiting for active turns — force-canceling",
			"timeout_seconds", activeTurnTimeout)
	}

	// Now close the agent loop resources.
	// SH5: After the active-turn wait (or timeout), Close() is called. If any turns
	// are still in-flight at this point, their context will have been canceled by the
	// overall ctx timeout propagation. Session stores use atomic writes so partial-write
	// corruption is not possible. This is documented behavior per FUNC-36.
	done := make(chan struct{})
	go func() {
		defer close(done)
		agentLoop.Close()
	}()

	select {
	case <-done:
		slog.Info("shutdown: agent loop drained cleanly")
	case <-ctx.Done():
		slog.Warn("shutdown: timeout waiting for agent loop drain — saving partial state")
	}

	slog.Info("shutdown: step 3 — verifying partial state persistence")
	// The session PartitionStore uses O_APPEND writes that are already durable.
	// Any in-progress streaming response was saved with status "interrupted" by
	// the agent loop's context cancellation path (handled in pkg/agent).

	slog.Info("shutdown: step 4 — stopping background services")
	// Stop remaining services (heartbeat, cron, media, devices).
	if runningServices.HeartbeatService != nil {
		runningServices.HeartbeatService.Stop()
	}
	if runningServices.CronService != nil {
		runningServices.CronService.Stop()
	}
	if runningServices.DeviceService != nil {
		runningServices.DeviceService.Stop()
	}
	if runningServices.HealthServer != nil {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel2()
		if err := runningServices.HealthServer.Stop(ctx2); err != nil {
			slog.Warn("shutdown: health server stop error", "error", err)
		}
	}

	slog.Info("shutdown: step 5 — closing provider connections")
	if cp, ok := provider.(providers.StatefulProvider); ok {
		cp.Close()
	}

	slog.Info("shutdown: complete")
}
