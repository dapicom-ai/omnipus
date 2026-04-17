# pkg/agent/testutil

Shared test infrastructure for all Plan-3 PRs. Import path:

```
github.com/dapicom-ai/omnipus/pkg/agent/testutil
```

## What this package provides

| File | Purpose |
|---|---|
| `scenario_provider.go` | `ScenarioProvider` — scriptable multi-turn LLM provider for unit tests |
| `gateway_harness.go` | `TestGateway` — boots the real gateway via `RunContext` on an ephemeral port |
| `options.go` | Functional options consumed by `StartTestGateway` |

## What the harness does

`StartTestGateway` boots the **real** `gateway.RunContext` in a goroutine, wired
to an ephemeral `127.0.0.1` port allocated by the OS. It:

1. Creates a `t.TempDir()` as `OMNIPUS_HOME`.
2. Injects a fixed `OMNIPUS_MASTER_KEY` so credentials unlock without a TTY.
3. Writes a minimal `config.json` (host, port, model name = "scripted-model").
4. Installs a `ScenarioProvider` via `SetTestProviderOverride` so the agent loop
   returns scripted responses instead of calling a real LLM.
5. Polls `GET /health` until 200 (max 5 s) before returning to the caller.
6. Registers `t.Cleanup(gw.Close)` — cancels the context and waits up to 10 s
   for `RunContext` to return. Reports `t.Errorf` if it times out (goroutine leak)
   or if `RunContext` exits with an error after becoming ready (boot error surfaced).

The gateway runs the full stack: HTTP/WebSocket server, agent loop, cron, heartbeat,
media store, and channel manager. Tests interact with it over `http://127.0.0.1:<port>`.

## Quick usage

```go
func TestMain(m *testing.M) {
    testutil.RegisterGatewayRunner(gateway.RunContext)
    testutil.RegisterProviderOverrideFuncs(
        gateway.SetTestProviderOverride,
        gateway.ClearTestProviderOverride,
    )
    os.Exit(m.Run())
}

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

## Available options

| Option | Effect |
|---|---|
| `WithScenario(p)` | Use a pre-built ScenarioProvider |
| `WithAllowEmpty()` | Allow boot without a default model configured |
| `WithBearerAuth()` | Seed a bearer token; sets `gw.BearerToken` |
| `WithAgents(list)` | Pre-seed a custom agents list |
| `WithSandboxConfig(cfg)` | Override sandbox settings |

## Acceptance criteria

See [temporal-puzzling-melody.md](../../../docs/plans/temporal-puzzling-melody.md)
§1 acceptance contracts for the full list of behaviors this harness is designed to validate.
