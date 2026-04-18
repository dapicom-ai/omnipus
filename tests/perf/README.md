# tests/perf — Performance & SLO Tests

This package contains Go benchmarks and SLO gate tests for the Omnipus gateway.
C1 writes benchmarks and SLO tests; C2 (this PR) provides the 2000-session load
harness and the CI workflow wiring.

---

## Running benchmarks locally

```bash
CGO_ENABLED=0 go test -tags goolm,stdjson \
  -bench=. -benchtime=3s -benchmem \
  -run='^$' -timeout 10m -v ./tests/perf/...
```

Results land in `tests/perf/results/bench-<YYYY-MM-DD>.txt` when run via the
nightly workflow. Local runs print to stdout.

---

## Running SLO gate tests locally

SLO gates check boot latency, per-turn latency, transcript integrity, media
resolution, and compaction bounds:

```bash
CGO_ENABLED=0 go test -tags goolm,stdjson \
  -run '^(TestBootUnder1Second|TestPerTurnSLO|TestNoTranscriptDataLoss|TestMediaResolveSLO|TestCompactionBoundsMemory)$' \
  -timeout 5m -v ./tests/perf/...
```

These run on every PR via the `perf-smoke` CI job (no external env var required).

---

## Running the 2000-session load test locally

The load test is guarded by an environment variable to keep `go test ./...` fast:

```bash
OMNIPUS_RUN_LOAD_TEST=1 CGO_ENABLED=0 go test -tags goolm,stdjson \
  -run '^TestLoad2000Sessions$' \
  -timeout 12m -v ./tests/perf/...
```

Expected runtime: ~6 minutes (40 s ramp + 5 min hold + 10 s teardown).

**Tip:** Raise your open-file limit first on Linux/macOS to avoid
`too many open files` errors during the ramp:

```bash
ulimit -n 4096
```

Results are written to `tests/perf/results/load-2000-<RFC3339>.json`.

---

## Where results land

| Output | Path |
|---|---|
| Benchmark text | `tests/perf/results/bench-<YYYY-MM-DD>.txt` |
| Load test JSON | `tests/perf/results/load-2000-<RFC3339>.json` |

The `results/` directory is committed to the repo for trend analysis (see
`.gitignore` for the explicit allow rule). The nightly CI job commits results
automatically via the `omnipus-perf-bot` git identity.

---

## Nightly CI job

The `.github/workflows/perf-nightly.yml` workflow runs at 03:00 UTC daily and:

1. Builds the SPA and syncs it into the Go embed target.
2. Raises `ulimit -n 4096` for the load test.
3. Runs all SLO gate tests + `TestLoad2000Sessions` (timeout 15 min).
4. Runs all benchmarks and tees output to `results/bench-<date>.txt`.
5. Commits any new result files to the current branch.
6. Uploads the `results/` directory as a GitHub Actions artifact (30-day retention).

The workflow can also be triggered manually via `workflow_dispatch`.

---

## SLO table (Plan 3 §1 Axis-6)

| SLO | Threshold | Test |
|---|---|---|
| Boot latency | < 1 s (hard ceiling 1.5 s) | `TestBootUnder1Second` |
| p95 first-token (2000 sessions) | < 1 s | `TestLoad2000Sessions` |
| Peak RSS (2000 sessions) | < 500 MB | `TestLoad2000Sessions` |
| Dropped frames | 0 | `TestLoad2000Sessions` |
| Goroutine leak after teardown | < 10 | `TestLoad2000Sessions` |
| Per-turn latency (single session) | see `TestPerTurnSLO` | `TestPerTurnSLO` |
| Transcript data loss | zero | `TestNoTranscriptDataLoss` |
| Media resolve latency | see `TestMediaResolveSLO` | `TestMediaResolveSLO` |
| Compaction memory bounds | see `TestCompactionBoundsMemory` | `TestCompactionBoundsMemory` |
