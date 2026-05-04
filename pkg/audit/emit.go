// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors
//
// audit.Emit family — best-effort audit emission helpers that bump the
// audit-skipped counter on Log failure so /health audit_degraded actually
// surfaces the gap (CRIT-6).

package audit

import "log/slog"

// EmitEntry writes one Entry to logger and bumps the audit-skipped counter
// on Log failure. Use this from agent-loop callsites instead of calling
// logger.Log directly so a write failure increments IncSkipped — which is
// what /health audit_degraded reads.
//
// Background: before this helper, only pkg/tools/web_serve called
// audit.IncSkipped on Log failure. Every other agent-loop site (rate-limit
// denial, prompt_guard sanitize, turn_aborted_synthetic_loop, FR-048
// shutdown, FR-069 SIGKILL recovery, tool.assembly.duplicate_name,
// tool.policy.deny.attempted) fell back to a slog.Warn and left the counter
// alone — so /health audit_degraded reported false even when audit was
// silently dropping rows. CRIT-6 closes that observability gap.
//
// Contract:
//   - logger == nil  → no-op (audit explicitly disabled by operator).
//     IncSkipped is NOT bumped — disable is not a failure.
//   - entry == nil   → no-op (defensive, mirrors Logger.Log).
//   - Log error      → slog.Warn + IncSkipped(entry.Event, entry.Decision).
//     Returns nil to caller so tool execution is never blocked by an
//     audit-emit failure (preserves the receiver-nil contract of
//     "best-effort, never block tool execution").
func EmitEntry(logger *Logger, entry *Entry) {
	if logger == nil || entry == nil {
		return
	}
	if err := logger.Log(entry); err != nil {
		// Counter bump uses the entry's Event as the "tool" label so
		// /metrics can disambiguate which audit emission family was
		// dropped (rate_limit, policy_eval, exec, etc.). For empty
		// Event the bump uses "unknown_event" — Logger.Log already
		// rejected the entry in that case.
		toolLabel := entry.Event
		if toolLabel == "" {
			toolLabel = "unknown_event"
		}
		slog.Warn("audit emit failed",
			"event", entry.Event,
			"decision", entry.Decision,
			"agent_id", entry.AgentID,
			"error", err)
		IncSkipped(toolLabel, entry.Decision)
	}
}
