// Package testutil provides shared performance-test orchestration helpers for
// the Omnipus load and SLO test suites. It is intentionally not a _test.go
// file so it can be imported from any _test.go in the repo.
//
// All helpers are pure Go stdlib — no external dependencies are required.
package testutil

import (
	"runtime"
	"sort"
	"sync"
	"time"
)

// LoadMetrics captures the counters a load run cares about.
// All fields are private; use the provided methods to record observations and
// the getter methods to read results after Finalize has been called.
type LoadMetrics struct {
	// goroutinesBefore is the goroutine count sampled before the load ramp.
	goroutinesBefore int
	// goroutinesAfter is the goroutine count sampled after teardown + grace period.
	goroutinesAfter int

	// sessionsOpened is the number of WebSocket sessions successfully opened.
	sessionsOpened int
	// messagesRecv is the total number of assistant frames received.
	messagesRecv int
	// droppedFrames is the count of frames that arrived out of order, were
	// corrupt, or whose connection was lost before the frame was received.
	droppedFrames int

	// firstTokenLatencies holds one duration per session — the wall-clock time
	// from WS send to receipt of the first assistant message frame.
	firstTokenLatencies []time.Duration
	// doneFrameLatencies holds one duration per session — the wall-clock time
	// from WS send to receipt of the final "done" frame.
	doneFrameLatencies []time.Duration

	// peakRSSBytes is the highest RSS sample taken during the run (best-effort,
	// derived from runtime.MemStats — see SampleRSS for caveats).
	peakRSSBytes uint64
	// duration is the total wall-clock time of the load run.
	duration time.Duration

	mu sync.Mutex
}

// NewLoadMetrics creates a LoadMetrics seeded with the goroutine count
// sampled before the load ramp. Callers should call CountGoroutines() and
// pass the result here before launching any load goroutines.
func NewLoadMetrics(goroutinesBefore int) *LoadMetrics {
	return &LoadMetrics{goroutinesBefore: goroutinesBefore}
}

// RecordFirstToken appends d to the first-token latency series. It is safe
// to call from multiple goroutines concurrently.
func (m *LoadMetrics) RecordFirstToken(d time.Duration) {
	m.mu.Lock()
	m.firstTokenLatencies = append(m.firstTokenLatencies, d)
	m.mu.Unlock()
}

// RecordDoneFrame appends d to the done-frame latency series. It is safe
// to call from multiple goroutines concurrently.
func (m *LoadMetrics) RecordDoneFrame(d time.Duration) {
	m.mu.Lock()
	m.doneFrameLatencies = append(m.doneFrameLatencies, d)
	m.mu.Unlock()
}

// IncSessionsOpened increments the opened-session counter by one. Safe for
// concurrent use.
func (m *LoadMetrics) IncSessionsOpened() {
	m.mu.Lock()
	m.sessionsOpened++
	m.mu.Unlock()
}

// IncMessagesRecv increments the received-message counter by one. Safe for
// concurrent use.
func (m *LoadMetrics) IncMessagesRecv() {
	m.mu.Lock()
	m.messagesRecv++
	m.mu.Unlock()
}

// IncDropped increments the dropped-frame counter by one. Safe for concurrent use.
func (m *LoadMetrics) IncDropped() {
	m.mu.Lock()
	m.droppedFrames++
	m.mu.Unlock()
}

// Finalize records the post-run goroutine count and total duration. Call once
// after all load goroutines have returned and the grace period has elapsed.
func (m *LoadMetrics) Finalize(goroutinesAfter int, duration time.Duration) {
	m.mu.Lock()
	m.goroutinesAfter = goroutinesAfter
	m.duration = duration
	m.mu.Unlock()
}

// SessionsOpened returns the total number of WebSocket sessions successfully opened.
func (m *LoadMetrics) SessionsOpened() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionsOpened
}

// MessagesRecv returns the total number of assistant frames received.
func (m *LoadMetrics) MessagesRecv() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messagesRecv
}

// DroppedFrames returns the count of dropped frames.
func (m *LoadMetrics) DroppedFrames() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.droppedFrames
}

// GoroutinesBefore returns the goroutine count sampled before the load ramp.
func (m *LoadMetrics) GoroutinesBefore() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.goroutinesBefore
}

// GoroutinesAfter returns the goroutine count sampled after teardown.
func (m *LoadMetrics) GoroutinesAfter() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.goroutinesAfter
}

// PeakRSSBytes returns the highest RSS sample taken during the run.
func (m *LoadMetrics) PeakRSSBytes() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peakRSSBytes
}

// Duration returns the total wall-clock time of the load run.
func (m *LoadMetrics) Duration() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.duration
}

// P95FirstToken returns the 95th-percentile first-token latency.
// Returns 0 if no first-token latencies have been recorded.
func (m *LoadMetrics) P95FirstToken() time.Duration {
	m.mu.Lock()
	cp := append([]time.Duration(nil), m.firstTokenLatencies...)
	m.mu.Unlock()
	return Percentile(cp, 0.95)
}

// P95DoneFrame returns the 95th-percentile done-frame latency.
// Returns 0 if no done-frame latencies have been recorded.
func (m *LoadMetrics) P95DoneFrame() time.Duration {
	m.mu.Lock()
	cp := append([]time.Duration(nil), m.doneFrameLatencies...)
	m.mu.Unlock()
	return Percentile(cp, 0.95)
}

// P99DoneFrame returns the 99th-percentile done-frame latency.
// Returns 0 if no done-frame latencies have been recorded.
func (m *LoadMetrics) P99DoneFrame() time.Duration {
	m.mu.Lock()
	cp := append([]time.Duration(nil), m.doneFrameLatencies...)
	m.mu.Unlock()
	return Percentile(cp, 0.99)
}

// UpdatePeakRSS samples the current RSS and updates peakRSSBytes if the
// current sample is higher than the stored peak.
func (m *LoadMetrics) UpdatePeakRSS() {
	rss := SampleRSS()
	m.mu.Lock()
	if rss > m.peakRSSBytes {
		m.peakRSSBytes = rss
	}
	m.mu.Unlock()
}

// Percentile returns the p-th percentile (p in [0, 1]) of the latency slice.
// The function operates on a copy of the provided slice — the caller's original
// slice is never mutated. p is clamped to [0, 1].
//
// Returns 0 if the slice is empty or p <= 0.
func Percentile(latencies []time.Duration, p float64) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	if p <= 0 {
		return 0
	}

	// Sort a copy so the caller's slice order is preserved.
	cp := append([]time.Duration(nil), latencies...)
	sort.Slice(cp, func(i, j int) bool {
		return cp[i] < cp[j]
	})

	if p >= 1 {
		return cp[len(cp)-1]
	}

	// Nearest-rank method — ceiling(p * n).
	idx := int(p*float64(len(cp)+1)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
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
// SampleRSS acquires the runtime heap lock and briefly suspends allocation;
// it's safe to call from a non-hot path, but avoid per-iteration use in hot
// loops. (Note: runtime.ReadMemStats has NOT triggered a stop-the-world pause
// since Go 1.9.)
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
