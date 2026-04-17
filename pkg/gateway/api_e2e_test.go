//go:build !cgo

// Plan 3 PR-A — API-level E2E tests using the in-process test gateway harness.
//
// These 11 tests cover the full gateway → agent loop → WebSocket → session pipeline.
// Five are from Plan 1 Layer 3; six are Plan 3 Axis-3 extensions.
//
// DEPENDENCY: These tests require pkg/agent/testutil/gateway_harness.go (A1 work).
// Until that file lands, every test is t.Skip'd with an explicit tracking reference.
// When A1 lands the harness, remove the t.Skip calls and implement the test bodies.
//
// Each t.Skip cites:
//   - The plan section that defines the test (temporal-puzzling-melody.md §Layer 3 / §4 Axis-3)
//   - The acceptance decision from Plan 3 §1 that the test enforces
//   - The A1 prerequisite (StartTestGateway must exist)

package gateway

import (
	"os"
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
	t.Skip("blocked on pkg/agent/testutil/gateway_harness.go (A1) — tracked in Plan 3 §Layer 3 test 1")
	// When StartTestGateway lands:
	//   gw := testutil.StartTestGateway(t, testutil.WithAllowEmpty(), testutil.WithScenario(
	//     testutil.NewScenario().WithText("Hello from the agent"),
	//   ))
	//
	//   1. POST /api/v1/onboarding/complete (admin user + provider config).
	//   2. POST /api/v1/auth/login → extract bearer token.
	//   3. Open WS /api/v1/ws with Authorization: Bearer <token>.
	//   4. Send chat message JSON frame.
	//   5. Read frames until "done" event_type received; assert "token" frames arrived.
	//   6. GET /api/v1/sessions → assert len(sessions) == 1.
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
	t.Skip("blocked on pkg/agent/testutil/gateway_harness.go (A1) — tracked in Plan 3 §Layer 3 test 2 (regression ebb976d)")
	// When StartTestGateway lands:
	//   gw := testutil.StartTestGateway(t, testutil.WithBearerAuth(), testutil.WithScenario(
	//     testutil.NewScenario().WithText("screenshot stored"),
	//   ))
	//
	//   1. Store a media ref via gw.Provider (or inject via test endpoint).
	//   2. Resolve the media URL — assert 200 OK with non-empty body.
	//   3. POST /api/v1/admin/reload.
	//   4. Resolve the same media URL again — must still be 200 OK with same body.
	//   5. Differentiation: a non-existent media ref returns 404 (not 200).
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
	t.Skip("blocked on pkg/agent/testutil/gateway_harness.go (A1) — tracked in Plan 3 §Layer 3 test 3")
	// When StartTestGateway lands:
	//   gw1 := testutil.StartTestGateway(t, testutil.WithBearerAuth(), testutil.WithScenario(...))
	//   Send 3 messages, capture session ID.
	//   gw1.Close() // triggers graceful shutdown + flush.
	//   gw2 := testutil.StartTestGateway(t, testutil.WithBearerAuth(),
	//     testutil.WithHomeDir(gw1.HomeDir)) // reuse same home dir
	//   GET /api/v1/sessions/<id> → assert transcript has 3 entries.
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
	t.Skip("blocked on pkg/agent/testutil/gateway_harness.go (A1) — tracked in Plan 3 §Layer 3 test 4")
	// When StartTestGateway lands:
	//   1. Boot with exec policy = deny.
	//   2. GET /api/v1/agents/main/tools → verify exec policy == "deny".
	//   3. Edit gw.ConfigPath to set exec policy = "allow".
	//   4. POST /api/v1/admin/reload.
	//   5. GET /api/v1/agents/main/tools → verify exec policy == "allow".
	//   6. Differentiation: the policy value must change (not be the same as before reload).
}

// TestRateLimitHeadersExposed verifies that rate-limit responses include informative headers.
//
// BDD: Given rate limit exceeded, Then response headers include X-RateLimit-Remaining and X-RateLimit-Reset.
//
// Traces to: temporal-puzzling-melody.md §Layer 3, test 5
// Acceptance: Plan 3 §1 — per-agent llm_calls_per_hour enforced
func TestRateLimitHeadersExposed(t *testing.T) {
	t.Skip("blocked on pkg/agent/testutil/gateway_harness.go (A1) — tracked in Plan 3 §Layer 3 test 5")
	// When StartTestGateway lands:
	//   Boot with MaxAgentLLMCallsPerHour=1.
	//   Send 2 chat messages rapidly; second should trigger rate limit.
	//   Assert response headers: X-RateLimit-Remaining present and <= 0, X-RateLimit-Reset present.
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
	t.Skip("blocked on pkg/agent/testutil/gateway_harness.go (A1) — tracked in Plan 3 §4 Axis-3 test 6")
	// When StartTestGateway lands:
	//   1. Create two sessions with transcript files dated 2 days ago (time-spoof or direct JSONL write).
	//   2. Update config retention.session_days = 1.
	//   3. POST /api/v1/admin/reload (triggers retroactive sweep).
	//   4. GET /api/v1/sessions → assert 0 sessions returned.
	//   5. Assert JSONL files are deleted from gw.HomeDir/sessions/.
	//   Differentiation: a session from today must survive the sweep.
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
	t.Skip("blocked on pkg/agent/testutil/gateway_harness.go (A1) — tracked in Plan 3 §4 Axis-3 test 7")
	// When StartTestGateway lands:
	//   1. Create custom agent "alpha" via POST /api/v1/agents.
	//   2. Create a session with agent "alpha".
	//   3. Send one message to populate the transcript.
	//   4. DELETE /api/v1/agents/alpha.
	//   5. GET /api/v1/sessions/<id> → assert 200, transcript present, agent_removed=true in response.
	//   6. POST /api/v1/sessions/<id>/messages → assert 422 (input disabled for removed-agent sessions).
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
	t.Skip("blocked on pkg/agent/testutil/gateway_harness.go (A1) — tracked in Plan 3 §4 Axis-3 test 8")
	// When StartTestGateway lands with audit_log=true:
	//   Drive 50 tool calls, 10 LLM calls, 3 handoffs, 5 bad-auth attempts via HTTP.
	//   Read gw.HomeDir/system/audit.jsonl line by line.
	//   Count entries per event_kind; assert total == 68.
	//   Assert specific kinds present: "tool_call" x50, "llm_call" x10, "handoff" x3 (or equiv), "auth_fail" x5.
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
	t.Skip("gated on /api/v1/version endpoint — tracked in Plan 3 §4 Axis-3 test 9; implement after /version endpoint lands")
	// When /api/v1/version endpoint and gateway_harness exist:
	//   gw := testutil.StartTestGateway(t, testutil.WithAllowEmpty())
	//   resp := gw.HTTPClient.Get(gw.URL + "/api/v1/version")
	//   assert.Equal(t, 200, resp.StatusCode)
	//   buildHeader := resp.Header.Get("X-Omnipus-Build")
	//   assert.NotEmpty(t, buildHeader, "X-Omnipus-Build header must be set")
	//   assert.Len(t, buildHeader, 40, "build hash must be a 40-char git SHA or similar")
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
	t.Skip("blocked on pkg/agent/testutil/gateway_harness.go (A1) — tracked in Plan 3 §4 Axis-3 test 10")
	// When StartTestGateway lands:
	//   gw := testutil.StartTestGateway(t, testutil.WithBearerAuth(), testutil.WithScenario(...))
	//   wsA := dialWS(gw.URL + "/api/v1/ws", gw.BearerToken)
	//   wsB := dialWS(gw.URL + "/api/v1/ws", gw.BearerToken)
	//   wsA.WriteJSON(chatMessage("hello from A"))
	//   deadline := time.Now().Add(500 * time.Millisecond)
	//   wsB.SetReadDeadline(deadline)
	//   frame := wsB.ReadJSON(...)
	//   assert.Contains(t, frame.Content, "hello from A")
}

// TestCredentialPermFatal verifies that a master.key file with mode 0644 causes
// a fatal boot error with a specific error message.
//
// BDD: Given master.key at mode 0644, When gateway boots, Then it exits with a perm error.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-3 test 11
// Acceptance: Plan 3 §1 — "Credential file perms: 0600 enforced at boot; non-compliant → fatal exit"
func TestCredentialPermFatal(t *testing.T) {
	// This test can run without the full gateway harness because it only needs to
	// verify the filesystem permission check, not a running gateway.
	//
	// Traces to: temporal-puzzling-melody.md §4 Axis-3 test 11
	// Acceptance: Plan 3 §1 permission enforcement decision

	tmpDir := t.TempDir()
	keyPath := tmpDir + "/master.key"

	// Write a key file at mode 0644 (insecure).
	if err := os.WriteFile(keyPath, []byte("deadbeef"), 0o644); err != nil {
		t.Fatalf("failed to create test key file: %v", err)
	}

	// Verify the file is actually 0644 (sanity check — some OSes may mask this).
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	actualMode := info.Mode().Perm()

	// The 0644 mode means group+other can read: (actualMode & 0o044) != 0.
	// The security check must detect this and refuse to use the key.
	insecure := (actualMode & 0o044) != 0
	if !insecure {
		t.Skip("OS masked the 0644 permission — cannot verify security check on this platform")
	}

	// Assert: mode 0644 IS detectable as insecure (this is what the boot check enforces).
	// The actual gateway boot rejection is tested via StartTestGateway in the skip'd block below.
	// Here we prove the permission bit that the security check reads is non-zero.
	groupReadable := (actualMode & 0o040) != 0
	otherReadable := (actualMode & 0o004) != 0
	if !groupReadable && !otherReadable {
		t.Fatal("BLOCKED: file written at 0644 has no group/other read bits — test cannot exercise security check")
	}

	t.Skip("blocked on pkg/agent/testutil/gateway_harness.go (A1) for full boot-failure assertion — " +
		"permission bit verification above passes; tracked in Plan 3 §4 Axis-3 test 11")
	// When StartTestGateway lands:
	//   Boot with master.key at 0644 → assert gateway exits with error containing "0600".
}
