// Package testutil provides shared performance-test orchestration helpers for
// the Omnipus load and SLO test suites. It is intentionally not a _test.go
// file so it can be imported from any _test.go in the repo.
//
// All helpers are pure Go stdlib — no external dependencies are required.
package testutil

import (
	"runtime"
	"sort"
	"time"
)

// LoadMetrics captures the counters a load run cares about.
type LoadMetrics struct {
	// SessionsOpened is the number of WebSocket sessions successfully opened.
	SessionsOpened int
	// MessagesSent is the total number of user messages sent across all sessions.
	MessagesSent int
	// MessagesRecv is the total number of assistant frames received.
	MessagesRecv int
	// DroppedFrames is the count of frames that arrived out of order, were
	// corrupt, or whose connection was lost before the frame was received.
	DroppedFrames int
	// FirstTokenLatency holds one duration per session — the wall-clock time
	// from WS send to receipt of the first assistant message frame.
	FirstTokenLatency []time.Duration
	// GoroutinesBefore is the goroutine count sampled before the load ramp.
	GoroutinesBefore int
	// GoroutinesAfter is the goroutine count sampled after teardown + grace period.
	GoroutinesAfter int
	// PeakRSSBytes is the highest RSS sample taken during the run (best-effort,
	// derived from runtime.MemStats — see SampleRSS for caveats).
	PeakRSSBytes uint64
	// Duration is the total wall-clock time of the load run.
	Duration time.Duration
}

// Percentile returns the p-th percentile (p in [0, 1]) of the latency slice.
// The slice is sorted in-place; callers that care about original order must
// pass a copy.
//
// Returns 0 if the slice is empty. p is clamped to [0, 1].
func Percentile(latencies []time.Duration, p float64) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return latencies[len(latencies)-1]
	}

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	// Nearest-rank method — ceiling(p * n).
	idx := int(p*float64(len(latencies)+1)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(latencies) {
		idx = len(latencies) - 1
	}
	return latencies[idx]
}

// CountGoroutines returns the current number of live goroutines reported by
// the Go runtime. Use before and after a load run to detect goroutine leaks.
func CountGoroutines() int {
	return runtime.NumGoroutine()
}

// SampleRSS returns a best-effort estimate of current process RSS in bytes.
// It uses runtime.MemStats (HeapInuse + StackInuse + MSpanInuse + MCacheInuse
// + GCSys + OtherSys) which tracks Go-managed memory. OS-level RSS may be
// higher due to mapped files, C extensions, or fragmentation; this is
// sufficient for perf trend detection but not exact accounting.
//
// The function forces a GC pause (runtime.ReadMemStats calls STW) — only call
// at sample intervals, not in hot paths.
func SampleRSS() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	// Sum the major live-memory buckets reported by the Go runtime.
	return ms.HeapInuse +
		ms.StackInuse +
		ms.MSpanInuse +
		ms.MCacheInuse +
		ms.GCSys +
		ms.OtherSys
}
