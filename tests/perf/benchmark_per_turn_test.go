//go:build !cgo

package perf

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/dapicom-ai/omnipus/pkg/agent/testutil"
)

// wsClientFrame mirrors the gateway's internal frame type so we can send messages.
type wsClientFrame struct {
	Type      string `json:"type"`
	Token     string `json:"token,omitempty"`
	Content   string `json:"content,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
}

// wsServerFrame mirrors the gateway's outbound frame type.
type wsServerFrame struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
}

// perTurnLatencies holds timing from one WS message exchange.
type perTurnLatencies struct {
	firstTokenMs float64
	doneMs       float64
}

// dialAndAuth opens a WebSocket connection to the gateway and sends the auth frame.
// In dev mode (DevModeBypass=true, no OMNIPUS_BEARER_TOKEN), any non-empty token is accepted.
func dialAndAuth(wsURL, token string) (*websocket.Conn, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", wsURL, err)
	}

	authData, err := json.Marshal(wsClientFrame{Type: "auth", Token: token})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("marshal auth frame: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authData); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write auth frame: %w", err)
	}
	return conn, nil
}

// sendAndMeasure sends a chat message and measures first-token and done-frame latency.
//   - firstTokenMs: time from send to the first "token" frame (or "done" if no token frame).
//   - doneMs: time from send to the "done" frame.
func sendAndMeasure(conn *websocket.Conn, content string) (perTurnLatencies, error) {
	msgData, err := json.Marshal(wsClientFrame{Type: "message", Content: content})
	if err != nil {
		return perTurnLatencies{}, fmt.Errorf("marshal message frame: %w", err)
	}

	conn.SetWriteDeadline(time.Now().Add(30 * time.Second)) //nolint:errcheck
	sendAt := time.Now()
	if err := conn.WriteMessage(websocket.TextMessage, msgData); err != nil {
		return perTurnLatencies{}, fmt.Errorf("write message frame: %w", err)
	}

	var lat perTurnLatencies
	firstSeen := false

	for {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second)) //nolint:errcheck
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return perTurnLatencies{}, fmt.Errorf("read frame: %w", err)
		}
		now := time.Now()

		var frame wsServerFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue // skip malformed frames
		}

		// Record first visible assistant content.
		if !firstSeen && (frame.Type == "token" || (frame.Content != "" && frame.Type != "error")) {
			lat.firstTokenMs = float64(now.Sub(sendAt).Microseconds()) / 1000.0
			firstSeen = true
		}

		if frame.Type == "done" {
			lat.doneMs = float64(now.Sub(sendAt).Microseconds()) / 1000.0
			if !firstSeen {
				// done without prior token — count done as first token too.
				lat.firstTokenMs = lat.doneMs
			}
			return lat, nil
		}

		if frame.Type == "error" {
			return perTurnLatencies{}, fmt.Errorf("gateway error frame: %s", frame.Content)
		}
	}
}

// computePercentile returns the p-th percentile (0–100) of a sorted float64 slice.
// The slice must be sorted ascending before calling.
func computePercentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p / 100.0) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// logLatencyDistribution prints min/p50/p95/p99/max to the test log.
func logLatencyDistribution(t testing.TB, label string, sorted []float64) {
	t.Helper()
	if len(sorted) == 0 {
		t.Logf("%s distribution: empty", label)
		return
	}
	t.Logf("%s distribution (n=%d): min=%.2f ms  p50=%.2f ms  p95=%.2f ms  p99=%.2f ms  max=%.2f ms",
		label,
		len(sorted),
		sorted[0],
		computePercentile(sorted, 50),
		computePercentile(sorted, 95),
		computePercentile(sorted, 99),
		sorted[len(sorted)-1],
	)
}

// BenchmarkPerTurnScripted benchmarks per-turn WS latency using a ScenarioProvider
// with a fixed 100-token text response, eliminating model flake.
// Reports p50/p95/p99 for first-token and done-frame latency as custom metrics.
func BenchmarkPerTurnScripted(b *testing.B) {
	const response100Tokens = "The quick brown fox jumps over the lazy dog. " +
		"This is a scripted response with approximately one hundred tokens of content " +
		"to ensure a realistic payload size for latency measurement purposes. " +
		"The agent loop processes the message, invokes the scripted provider, " +
		"and streams the reply back through the WebSocket connection frame by frame."

	scenario := testutil.NewScenario()
	// Over-provision so we never hit ErrNoMoreResponses during the benchmark.
	for i := 0; i < b.N+1000; i++ {
		scenario = scenario.WithText(response100Tokens)
	}

	gw := startPerfGateway(b, scenario)

	wsURL := "ws" + strings.TrimPrefix(gw.URL, "http") + "/api/v1/chat/ws"
	conn, err := dialAndAuth(wsURL, "perf-token")
	if err != nil {
		b.Fatalf("BenchmarkPerTurnScripted: %v", err)
	}
	b.Cleanup(func() { conn.Close() })

	var firstTokenLatencies []float64
	var doneLatencies []float64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lat, err := sendAndMeasure(conn, fmt.Sprintf("turn %d", i))
		if err != nil {
			b.Fatalf("BenchmarkPerTurnScripted turn %d: %v", i, err)
		}
		firstTokenLatencies = append(firstTokenLatencies, lat.firstTokenMs)
		doneLatencies = append(doneLatencies, lat.doneMs)
	}
	b.StopTimer()

	if len(firstTokenLatencies) == 0 {
		return
	}

	sort.Float64s(firstTokenLatencies)
	sort.Float64s(doneLatencies)

	b.ReportMetric(computePercentile(firstTokenLatencies, 50), "p50_first_ms")
	b.ReportMetric(computePercentile(firstTokenLatencies, 95), "p95_first_ms")
	b.ReportMetric(computePercentile(firstTokenLatencies, 99), "p99_first_ms")
	b.ReportMetric(computePercentile(doneLatencies, 50), "p50_done_ms")
	b.ReportMetric(computePercentile(doneLatencies, 95), "p95_done_ms")
	b.ReportMetric(computePercentile(doneLatencies, 99), "p99_done_ms")
}

// TestPerTurnSLO runs 1000 turns via a scripted WS connection and asserts
// p95 first-token < 400 ms and p95 done-frame < 450 ms.
// Measured baseline on a fast dev box: flat ~100 ms distribution (p50=p95=p99)
// caused by the 100 ms idleTicker in pkg/agent/loop.go:912 (tracked in #92).
// GitHub-hosted ubuntu-latest runners have a much higher tail: p50=100 ms
// but p95=~290 ms and p99=~650 ms due to general runner CPU noise. The budget
// accommodates the CI reality; once #92 closes the idleTicker floor, tighten
// both budgets back to the original Plan 3 §1 aspirational 50 ms / 150 ms.
// If either budget is breached, the full latency distribution is logged before failing.
func TestPerTurnSLO(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	const (
		turns          = 1000
		p95FirstBudget = 400.0 // ms — CI-runner tolerant; see #92
		p95DoneBudget  = 450.0 // ms — same, with 50 ms headroom above first-token
	)

	const response100Tokens = "The quick brown fox jumps over the lazy dog. " +
		"This is a scripted response with approximately one hundred tokens of content " +
		"to ensure a realistic payload size for latency measurement purposes. " +
		"The agent loop processes the message, invokes the scripted provider, " +
		"and streams the reply back through the WebSocket connection frame by frame."

	scenario := testutil.NewScenario()
	for i := 0; i < turns; i++ {
		scenario = scenario.WithText(response100Tokens)
	}

	gw := testutil.StartTestGateway(t,
		testutil.WithAllowEmpty(),
		testutil.WithScenario(scenario),
	)

	wsURL := "ws" + strings.TrimPrefix(gw.URL, "http") + "/api/v1/chat/ws"
	conn, err := dialAndAuth(wsURL, "perf-token")
	if err != nil {
		t.Fatalf("TestPerTurnSLO: dial+auth: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	firstTokenMs := make([]float64, 0, turns)
	doneMs := make([]float64, 0, turns)

	for i := 0; i < turns; i++ {
		lat, err := sendAndMeasure(conn, fmt.Sprintf("SLO turn %d", i))
		if err != nil {
			t.Fatalf("TestPerTurnSLO turn %d: %v", i, err)
		}
		firstTokenMs = append(firstTokenMs, lat.firstTokenMs)
		doneMs = append(doneMs, lat.doneMs)
	}

	sort.Float64s(firstTokenMs)
	sort.Float64s(doneMs)

	p95First := computePercentile(firstTokenMs, 95)
	p95Done := computePercentile(doneMs, 95)

	logLatencyDistribution(t, "first-token", firstTokenMs)
	logLatencyDistribution(t, "done-frame", doneMs)

	if p95First > p95FirstBudget {
		t.Errorf(
			"TestPerTurnSLO FAILED: p95 first-token latency is %.2f ms, exceeds budget of %.0f ms. "+
				"See distribution above for full breakdown.",
			p95First, p95FirstBudget,
		)
	}

	if p95Done > p95DoneBudget {
		t.Errorf(
			"TestPerTurnSLO FAILED: p95 done-frame latency is %.2f ms, exceeds budget of %.0f ms. "+
				"See distribution above for full breakdown.",
			p95Done, p95DoneBudget,
		)
	}
}
