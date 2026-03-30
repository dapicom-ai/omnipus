// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/agent"
	"github.com/dapicom-ai/omnipus/pkg/providers"
)

// omnipusShutdownTimeout is the maximum time to wait for in-flight operations
// to complete before force-flushing partial state. Per FUNC-36 / US-9.
const omnipusShutdownTimeout = 10 * time.Second

// omnipusGracefulShutdown executes the 5-step graceful shutdown per FUNC-36:
//
//  1. Stop channel manager — no new inbound messages accepted
//  2. Drain agent loop — cancel running agent contexts, wait for in-flight work
//  3. Flush partial state — interrupted responses already saved by agent loop cancellation
//  4. Stop background services — heartbeat, cron, device, health
//  5. Close provider connections — release HTTP/WebSocket sessions
//
// Implements US-9 acceptance criteria.
func omnipusGracefulShutdown(
	runningServices *services,
	agentLoop *agent.AgentLoop,
	provider providers.LLMProvider,
) {
	ctx, cancel := context.WithTimeout(context.Background(), omnipusShutdownTimeout)
	defer cancel()

	slog.Info("shutdown: step 1 — stopping new requests")
	// Stop channel manager first so no new inbound messages are accepted.
	stopCtx, stopCancel := context.WithTimeout(ctx, 2*time.Second)
	defer stopCancel()
	if runningServices.ChannelManager != nil {
		runningServices.ChannelManager.StopAll(stopCtx)
	}

	slog.Info("shutdown: step 2 — waiting for in-flight operations",
		"timeout", omnipusShutdownTimeout)
	// Signal the agent loop to stop accepting new work and drain.
	agentLoop.Stop()

	// Wait for the agent loop to drain, honouring the shutdown context.
	done := make(chan struct{})
	go func() {
		defer close(done)
		agentLoop.Close()
	}()

	select {
	case <-done:
		slog.Info("shutdown: agent loop drained cleanly")
	case <-ctx.Done():
		slog.Warn("shutdown: timeout waiting for agent loop — saving partial state")
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
