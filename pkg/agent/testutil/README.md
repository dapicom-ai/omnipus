# pkg/agent/testutil

Shared test infrastructure for all Plan-3 PRs. Import path:

```
github.com/dapicom-ai/omnipus/pkg/agent/testutil
```

## What is in this package

| File | Purpose |
|---|---|
| `scenario_provider.go` | `ScenarioProvider` — a scriptable multi-turn LLM provider |
| `gateway_harness.go` | `TestGateway` — an in-process HTTP server on an ephemeral port |
| `options.go` | Functional options consumed by `StartTestGateway` |

## Quick usage

```go
func TestMyFeature(t *testing.T) {
    p := testutil.NewScenario().
        WithText("Hello!").
        WithToolCall("bash", `{"cmd":"ls"}`).
        WithText("All done.")

    gw := testutil.StartTestGateway(t, testutil.WithScenario(p))
    // gw.URL, gw.HTTPClient, gw.Provider are ready; cleanup is automatic.

    req, _ := gw.NewRequest(http.MethodGet, "/health", nil)
    resp, _ := gw.Do(req)
    // assert resp.StatusCode == 200
}
```

## Why it lives here

`pkg/agent/testutil/` is co-located with the primary consumer (`pkg/agent`).
Go convention: test-helper packages that are NOT `_test.go` files live adjacent
to the code they support so the import graph stays flat.

## Design note: lightweight harness, not full gateway.Run

`gateway.Run` blocks on OS signals and has no context-cancellation API, making
it unsuitable for in-process tests. The harness therefore spins up a minimal
`net/http.Server` (health + ready endpoints) on a real ephemeral port and wires
a `ScenarioProvider` directly into memory. Full REST-API coverage (agent loop,
sessions, config) can be added once `pkg/gateway` exports its internal `restAPI`
constructor.

## Which code this replaces

The one-response stub at `pkg/agent/mock_provider_test.go` (lines 9–26). A2
migrates the existing `pkg/agent` tests off that stub onto `ScenarioProvider`
in a follow-up commit.
