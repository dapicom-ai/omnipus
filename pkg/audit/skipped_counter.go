// Package audit — package-level counters for audit-skip events.
//
// B1.2(e): when a tool's audit-fail-closed mode is disabled (operator
// override) and a write fails, the tool emits slog.Error and proceeds —
// silently from the operator's perspective. This file exposes
// AuditSkippedTotal as a process-wide counter so /health (B1.2(f)) and
// /metrics can surface the degraded-audit state.
//
// Why here? The tools package can't depend on gateway's metrics
// infrastructure without an import cycle. The audit package is the
// natural home: it's the layer whose write failed, and every package
// that emits audit already imports pkg/audit. Health and gateway-metrics
// surface the value via a typed accessor (Snapshot) rather than
// re-exporting the atomic so callers can't tamper with it.

package audit

import "sync/atomic"

// auditSkippedCounters is the package-level state for B1.2(e). Stored as
// an atomic counter map keyed by "tool|decision" so we can support multiple
// labels without taking a mutex on the hot path. There are at most a handful
// of unique label combinations in practice (web_serve|allow, web_serve|deny,
// etc.), so a fixed-size struct is simpler than a sync.Map.
//
// Each tool that skips audit calls IncSkipped(tool, decision); the counter
// is read at /health and /metrics request time via SnapshotSkipped().
var auditSkippedCounters skippedCounters

// skippedCounters wraps the small set of label tuples we care about.
// Anything outside the named buckets is summed into "other" so we don't
// silently lose counts on misconfiguration.
type skippedCounters struct {
	webServeAllow atomic.Int64
	webServeDeny  atomic.Int64
	other         atomic.Int64
}

// IncSkipped increments the audit-skipped counter for the given (tool,
// decision) pair. Both labels are matched as case-sensitive strings so
// callers must pass canonical values (the audit.DecisionAllow / DecisionDeny
// constants are recommended).
//
// The counter is process-lifetime cumulative — there is no reset, by design.
// /health surfaces the absolute value; operators read deltas across two
// scrapes if they want a rate.
func IncSkipped(tool, decision string) {
	switch tool {
	case "web_serve":
		switch decision {
		case DecisionAllow:
			auditSkippedCounters.webServeAllow.Add(1)
		case DecisionDeny:
			auditSkippedCounters.webServeDeny.Add(1)
		default:
			auditSkippedCounters.other.Add(1)
		}
	default:
		auditSkippedCounters.other.Add(1)
	}
}

// SkippedSnapshot is a point-in-time read of the audit-skipped counters
// suitable for embedding in a /health or /metrics response. Field zero
// values are valid (no skips have occurred). Total is the sum of the
// labelled counters and is provided for convenience.
type SkippedSnapshot struct {
	WebServeAllow int64 `json:"web_serve_allow"`
	WebServeDeny  int64 `json:"web_serve_deny"`
	Other         int64 `json:"other"`
	Total         int64 `json:"total"`
}

// SnapshotSkipped returns the current counter values atomically. Each Load
// is independent, so a SkippedSnapshot returned during heavy concurrent
// IncSkipped calls may be slightly skewed across labels — acceptable for
// observability, never relied on for correctness.
func SnapshotSkipped() SkippedSnapshot {
	a := auditSkippedCounters.webServeAllow.Load()
	d := auditSkippedCounters.webServeDeny.Load()
	o := auditSkippedCounters.other.Load()
	return SkippedSnapshot{
		WebServeAllow: a,
		WebServeDeny:  d,
		Other:         o,
		Total:         a + d + o,
	}
}

// ResetSkippedForTest zeroes the counters. Test-only helper; production
// code must never reset (operators rely on monotonic counts).
func ResetSkippedForTest() {
	auditSkippedCounters.webServeAllow.Store(0)
	auditSkippedCounters.webServeDeny.Store(0)
	auditSkippedCounters.other.Store(0)
}
