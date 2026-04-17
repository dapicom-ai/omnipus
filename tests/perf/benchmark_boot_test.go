//go:build !cgo

package perf

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/agent/testutil"
)

// BenchmarkBootHealth measures the time from startPerfGateway return to the
// first successful GET /health 200 response. Allocates 1 MB per iteration to
// reduce GC noise in the ns/op reading. Reports boot_ms as a custom metric.
func BenchmarkBootHealth(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Allocate 1 MB to dampen GC jitter between iterations.
		sink := make([]byte, 1<<20)
		_ = sink

		start := time.Now()
		gw := startPerfGateway(b, nil)

		req, err := http.NewRequest(http.MethodGet, gw.URL+"/health", nil)
		if err != nil {
			b.Fatalf("BenchmarkBootHealth: NewRequest: %v", err)
		}
		resp, err := gw.HTTPClient.Do(req)
		if err != nil {
			b.Fatalf("BenchmarkBootHealth: GET /health: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("BenchmarkBootHealth: /health returned %d, want 200", resp.StatusCode)
		}

		elapsed := time.Since(start)
		b.ReportMetric(float64(elapsed.Milliseconds()), "boot_ms")

		gw.close(b)
	}
}

// TestBootUnder1Second is the SLO gate for boot latency. It runs StartTestGateway
// 10 times sequentially and asserts that every boot completes within 1500 ms.
// The 1 s budget is the aspirational target; 1500 ms is the hard CI ceiling.
func TestBootUnder1Second(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	const (
		runs        = 10
		sloHardMs   = 1500 // hard CI ceiling
		sloTargetMs = 1000 // aspirational target reported in metrics
	)

	var slowestMs int64
	var slowestRun int

	for i := 0; i < runs; i++ {
		start := time.Now()
		gw := testutil.StartTestGateway(t, testutil.WithAllowEmpty())

		req, err := gw.NewRequest(http.MethodGet, "/health", nil)
		if err != nil {
			t.Fatalf("TestBootUnder1Second run %d: NewRequest: %v", i+1, err)
		}
		resp, err := gw.Do(req)
		if err != nil {
			t.Fatalf("TestBootUnder1Second run %d: GET /health: %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("TestBootUnder1Second run %d: /health returned %d, want 200", i+1, resp.StatusCode)
		}

		elapsedMs := time.Since(start).Milliseconds()
		if elapsedMs > slowestMs {
			slowestMs = elapsedMs
			slowestRun = i + 1
		}

		gw.Close()

		if elapsedMs > sloHardMs {
			t.Errorf(
				"TestBootUnder1Second run %d: boot took %d ms, exceeds SLO ceiling of %d ms "+
					"(aspirational target: %d ms)",
				i+1, elapsedMs, sloHardMs, sloTargetMs,
			)
		}
	}

	t.Logf("TestBootUnder1Second: slowest boot was run %d at %d ms (SLO ceiling: %d ms, target: %d ms)",
		slowestRun, slowestMs, sloHardMs, sloTargetMs)

	if slowestMs > sloHardMs {
		t.Errorf(
			"TestBootUnder1Second FAILED: slowest run was %d ms on run %d; SLO ceiling is %d ms. "+
				"Investigate startup path — check gateway.go RunContext for blocking I/O or slow credential unlock.",
			slowestMs, slowestRun, sloHardMs,
		)
	} else if slowestMs > sloTargetMs {
		t.Logf("WARNING: slowest boot (%d ms) exceeded aspirational 1 s target but is within the %d ms hard ceiling",
			slowestMs, sloHardMs)
	}

	_ = fmt.Sprintf // ensure fmt is used
}
