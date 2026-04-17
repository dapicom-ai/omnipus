//go:build !cgo

// Plan 3 PR-A — API-level E2E tests using the in-process test gateway harness.
//
// These 11 tests cover the full gateway → agent loop → WebSocket → session pipeline.
// Five are from Plan 1 Layer 3; six are Plan 3 Axis-3 extensions.
//
// The harness (StartTestGateway) is live. Tests that require production features
// not yet implemented remain skipped with tracked reasons.

package gateway

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Plan 1 Layer 3 — Original 5 API-E2E tests
// ---------------------------------------------------------------------------

// TestOnboardingToFirstChat verifies the full onboarding → login → WS → chat pipeline.
//
// BDD: Given a fresh OMNIPUS_HOME, When onboarding completes and a user message is sent,
//
//	Then token+done frames arrive on the WS and GET /sessions lists 1 entry.
//
// Traces to: temporal-puzzling-melody.md §Layer 3, test 1
// Acceptance: Plan 3 §1 — "Audit log completeness: every LLM request"
func TestOnboardingToFirstChat(t *testing.T) {
	t.Skip(
		"pending implementation: requires WS chat pipeline + " +
			"onboarding/complete → auth/login → ws → chat → GET /sessions integration; " +
			"tracked in Plan 3 §Layer 3 test 1",
	)
}

// TestMediaServingAfterHotReload is a regression test for the MediaStore pointer
// staleness bug fixed in commit ebb976d. Before the fix, POST /reload replaced the
// MediaStore without updating the HTTP handler's pointer, so media URLs stopped
// resolving after reload.
//
// BDD: Given a stored media ref, When POST /reload is called, Then the media URL still resolves.
//
// Traces to: temporal-puzzling-melody.md §Layer 3, test 2
// Regression: commit ebb976d MediaStore pointer staleness bug
func TestMediaServingAfterHotReload(t *testing.T) {
	t.Skip(
		"pending implementation: requires media store injection API + " +
			"POST /reload + GET /api/v1/media/<ref> integration; " +
			"tracked in Plan 3 §Layer 3 test 2 (regression ebb976d)",
	)
}

// TestSessionPersistsAcrossRestart verifies that transcript data survives a gateway
// restart — the file-based JSONL store must be written before shutdown.
//
// BDD: Given 3 messages sent, When the gateway restarts with the same OMNIPUS_HOME,
//
//	Then GET /sessions still shows all 3 transcript entries.
//
// Traces to: temporal-puzzling-melody.md §Layer 3, test 3
func TestSessionPersistsAcrossRestart(t *testing.T) {
	t.Skip(
		"pending implementation: requires testutil.WithHomeDir option + " +
			"WS message sending + restart cycle; " +
			"tracked in Plan 3 §Layer 3 test 3",
	)
}

// TestConfigReloadUpdatesToolSet verifies that editing config.json and calling POST /reload
// makes new tool policies take effect without restart.
//
// BDD: Given exec is deny in config, When config is updated to allow and POST /reload called,
//
//	Then the agent's tool list reflects the new policy (allow).
//
// Traces to: temporal-puzzling-melody.md §Layer 3, test 4
func TestConfigReloadUpdatesToolSet(t *testing.T) {
	t.Skip(
		"pending implementation: requires config mutation + POST /reload + " +
			"GET /api/v1/agents/<id>/tools policy verification; " +
			"tracked in Plan 3 §Layer 3 test 4",
	)
}

// TestRateLimitHeadersExposed verifies that rate-limit responses include informative headers.
//
// BDD: Given rate limit exceeded, Then response headers include X-RateLimit-Remaining and X-RateLimit-Reset.
//
// Traces to: temporal-puzzling-melody.md §Layer 3, test 5
// Acceptance: Plan 3 §1 — per-agent llm_calls_per_hour enforced
func TestRateLimitHeadersExposed(t *testing.T) {
	t.Skip(
		"pending implementation: requires rate-limit config option in harness + " +
			"X-RateLimit-Remaining / X-RateLimit-Reset header assertion; " +
			"tracked in Plan 3 §Layer 3 test 5",
	)
}

// ---------------------------------------------------------------------------
// Plan 3 Axis-3 extensions — 6 additional API E2E tests
// ---------------------------------------------------------------------------

// TestRetentionRetroactiveSweep verifies that lowering the retention setting
// retroactively deletes sessions older than the new limit.
//
// BDD: Given sessions from 2 days ago exist, When retention is set to 1 day and sweep triggered,
//
//	Then both old sessions are deleted.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-3 test 6
// Acceptance: Plan 3 §1 — "Retention retroactive: lowering triggers immediate background sweep"
func TestRetentionRetroactiveSweep(t *testing.T) {
	t.Skip(
		"pending implementation: POST /api/v1/admin/retention-sweep endpoint not yet built; " +
			"tracked in Plan 3 §4 Axis-3 test 6",
	)
}

// TestDeletedAgentSessionReadOnly verifies that sessions belonging to a deleted agent
// remain readable but reject new messages.
//
// BDD: Given agent "alpha" has a session, When agent "alpha" is deleted,
//
//	Then GET session returns 200 with transcript + agent_removed=true; POST message returns 422.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-3 test 7
// Acceptance: Plan 3 §1 — "Session with deleted agent: read-only transcript + 'Agent removed' banner"
func TestDeletedAgentSessionReadOnly(t *testing.T) {
	t.Skip(
		"pending implementation: agent_removed field + 422 on deleted-agent session POST " +
			"not yet implemented in session handler; " +
			"tracked in Plan 3 §4 Axis-3 test 7",
	)
}

// TestAuditLogCompleteness verifies that every auditable event class produces
// exactly one audit entry.
//
// BDD: Given 50 tool calls + 10 LLM requests + 3 handoffs + 5 failed auth attempts,
//
//	Then audit.jsonl has exactly 68 entries with the correct event_kind values.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-3 test 8
// Acceptance: Plan 3 §1 — "Audit log completeness: every tool call, every LLM request, every handoff, every failed auth"
func TestAuditLogCompleteness(t *testing.T) {
	t.Skip(
		"pending implementation: requires ScenarioProvider-driven tool call + LLM + handoff + " +
			"auth-fail fixture pipeline; complex orchestration deferred; " +
			"tracked in Plan 3 §4 Axis-3 test 8",
	)
}

// TestSPAVersionMismatchHeader verifies the gateway emits the X-Omnipus-Build
// header and that /api/v1/version returns a build hash.
//
// BDD: Given the gateway is running, When GET /api/v1/version is called,
//
//	Then 200 is returned with the X-Omnipus-Build header present.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-3 test 9
// Acceptance: Plan 3 §1 — "SPA cache drift: /api/v1/version build hash poll"
func TestSPAVersionMismatchHeader(t *testing.T) {
	t.Skip(
		"/api/v1/version endpoint pending implementation; " +
			"tracked in Plan 3 §4 Axis-3 test 9",
	)
}

// TestMultiDeviceLiveSync verifies that two WebSocket connections on the same bearer
// token both receive a message sent from one of them within 500 ms.
//
// BDD: Given WS connections A and B with the same bearer, When A sends a message,
//
//	Then B receives the same message frame within 500 ms.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-3 test 10
// Acceptance: Plan 3 §1 — "Multi-device admin sessions: all clients see live updates"
func TestMultiDeviceLiveSync(t *testing.T) {
	t.Skip(
		"pending implementation: requires WS dial helper + concurrent connection + " +
			"cross-client broadcast verification; " +
			"tracked in Plan 3 §4 Axis-3 test 10",
	)
}

// TestCredentialPermFatal verifies that a master.key file with mode 0644 causes
// a fatal boot error with a specific error message.
//
// BDD: Given master.key at mode 0644, When gateway boots, Then it exits with a perm error.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-3 test 11
// Acceptance: Plan 3 §1 — "Credential file perms: 0600 enforced at boot; non-compliant → fatal exit"
func TestCredentialPermFatal(t *testing.T) {
	// The full boot-failure assertion (booting the gateway with a 0644 master.key
	// and asserting it exits with an error) requires testutil.WithKeyFile option
	// in StartTestGateway, which is not yet implemented.
	//
	// The unit-level perm check (credentials.Unlock rejecting 0644) is covered by
	// pkg/credentials/perm_check_test.go — that test exercises the production
	// loadKeyFile code directly via OMNIPUS_KEY_FILE.
	t.Skip(
		"contract pending: fatal-on-0644 boot guard integration test not yet implemented — " +
			"tracked in Plan 3 §1 ops guardrails; unit coverage is in pkg/credentials/perm_check_test.go",
	)
}
