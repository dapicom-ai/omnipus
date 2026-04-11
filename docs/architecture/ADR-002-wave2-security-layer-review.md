# ADR-002: Wave 2 Security Layer Architecture Review

**Status:** Accepted
**Date:** 2026-03-29 (updated 2026-04-10)
**Deciders:** architect, backend-lead, security-lead, qa-lead

## Context

Wave 2 implements the security foundation for Omnipus: kernel-level sandboxing (SEC-01, SEC-02, SEC-03), policy engine (SEC-04, SEC-05, SEC-07, SEC-11, SEC-12, SEC-17), audit logging (SEC-15, SEC-16), SSRF protection (SEC-24), rate limiting (SEC-26), exec approval (SEC-08), and exec HTTP proxy (SEC-28). This ADR documents the architecture review findings, assesses BRD compliance, and identifies issues to resolve before Wave 3.

## Decision

The Wave 2 security layer is **architecturally sound** in its core abstractions — `SandboxBackend` interface, immutable `Evaluator`, JSONL audit logger with rotation/retention, DNS-rebinding-aware SSRF checker, and sliding-window rate limiter. The component boundaries are clean and the integration pattern (policy evaluates → caller logs to audit) is functional.

However, **12 findings** require attention before Wave 3. Two are blockers, four are warnings, and six are notes.

## Findings

### Blockers

#### B-1: Seccomp has no actual BPF installation (SEC-02 non-compliance)

`BuildSeccompProgram()` creates a `SeccompProgram` struct with a map of blocked syscall names, but **never assembles or installs a BPF filter**. There is no call to `seccomp(SECCOMP_SET_MODE_FILTER, ...)` or equivalent via `unix.Syscall`. The struct's `Blocks()` method is a map lookup — it doesn't enforce anything at the kernel level.

SEC-02 requires: "Apply seccomp-BPF filters to agent tool execution processes to block privilege escalation, raw socket creation, module loading, and other dangerous syscalls. Implemented via `golang.org/x/sys/unix` BPF program assembly."

**Recommendation:** Implement actual BPF program assembly using `unix.SockFilter` / `unix.SockFprog` and install via `unix.Prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, ...)` or `unix.Syscall(SYS_SECCOMP, ...)`. The `SECCOMP_FILTER_FLAG_TSYNC` flag (SEC-03) must be set. This is P0 and blocks the Phase 1 exit criteria.

**BRD Ref:** SEC-02, SEC-03

#### B-2: Duplicated `matchGlob` and `firstToken` across packages

`matchGlob` is implemented identically in both `pkg/policy/evaluator.go:164` and `pkg/security/execapproval.go:54`. `firstToken` is similarly duplicated. This is a correctness risk — if one is fixed for a bug, the other may not be, leading to policy evaluation divergence between the two paths.

**Recommendation:** Extract to a shared `internal/globmatch` package or consolidate exec allowlist matching into the policy evaluator (which already has `EvaluateExec`). The `security.MatchExecAllowlist` function appears to be the legacy path — unify to one code path.

**BRD Ref:** CLAUDE.md hard constraint: "Single Go binary" — shared internal packages are the correct pattern.

### Warnings

#### W-1: FallbackBackend ignores access flags and has no-op ApplyToCmd

`FallbackBackend.CheckPath()` only checks path containment — it does not distinguish `AccessRead`, `AccessWrite`, or `AccessExecute`. An agent with read-only policy could write via the fallback backend without violation. Additionally, `ApplyToCmd` is a no-op, meaning child processes have zero filesystem restrictions on non-Linux platforms.

This means SEC-03 (child process inheritance) and the access-level granularity of SEC-01 are not enforced on fallback platforms. The BRD says "falls back to existing workspace-level checks" — but even workspace checks should distinguish read vs write.

**Recommendation:** `CheckPath` should accept an `access uint64` parameter and enforce it. `ApplyToCmd` should set environment variables or wrapper scripts that enforce path checks in child processes (best-effort, matching the "application-level enforcement" contract).

**BRD Ref:** SEC-01 (fallback), SEC-03

#### W-2: Duplicated type hierarchies in policy package

`SecurityConfig` / `SSRFPolicy` / `AuditPolicy` / `ExecPolicy` coexist with near-identical `SecurityConfigFile` / `SSRFConfig` / `AuditConfig` / `ExecConfig`. Both are used for the same purpose (parsing security config). The "legacy" types add ~100 lines of redundant code and create a maintenance burden — a field added to one may be forgotten in the other.

**Recommendation:** Unify to one type hierarchy. If backward compatibility requires different JSON shapes, use a migration function that converts old format to new, rather than parallel types.

**BRD Ref:** CLAUDE.md: "Minimal footprint" (code complexity contributes to cognitive overhead)

#### W-3: No automatic audit logging from policy evaluator

The policy evaluator returns a `Decision` but does not log it. The caller must manually construct an `audit.Entry` and call `logger.Log()`. This is error-prone — any code path that evaluates policy but forgets to log creates a gap in the audit trail. The BRD requires "All security-relevant events are logged" (SEC-15).

**Recommendation:** Create a `PolicyAuditor` or interceptor that wraps `Evaluator` and `Logger`, automatically logging every `EvaluateTool` / `EvaluateExec` decision. The e2e tests already demonstrate the manual wiring pattern — an interceptor would make it the only pattern.

**BRD Ref:** SEC-15, SEC-17

#### W-4: Global cost cap denial has no RetryAfterSeconds

When `CheckGlobalCostCap` denies a request, `RetryAfterSeconds` is 0 (zero value). SEC-26 specifies: "When any limit is hit, the operation is rejected with a cooldown — returns an error with `retry_after_seconds`." For cost caps, `retry_after_seconds` should be the time until UTC midnight (when the daily counter resets).

**Recommendation:** Compute `time.Until(nextMidnightUTC).Seconds()` and set it on the denial result.

**BRD Ref:** SEC-26

### Notes

#### N-1: No SEC-10 two-layer policy enforcement

SEC-10 (P2) requires separate sandbox-level and agent-level tool filters where both must permit a tool. The current evaluator has a single evaluation path. This is P2 and not in Phase 1 scope, but the architecture should accommodate it.

**Observation:** The `Evaluator` could accept a `SandboxPolicy` at construction and check it first, then check agent policy. No structural change needed — just a second lookup before the agent lookup.

**BRD Ref:** SEC-10

#### N-2: `extractHost` in SSRF checker uses hand-rolled URL parsing

`extractHost()` manually strips scheme, path, userinfo, and port via string operations instead of using `net/url.Parse()`. This is security-critical code where edge cases matter (URL encoding, backslash handling, authority parsing). The stdlib parser handles these correctly.

**Recommendation:** Replace with `url.Parse()` and extract `u.Hostname()`.

**BRD Ref:** SEC-24

#### N-3: Audit log `readLastLine` buffer limited to 4096 bytes

If the last line of `audit.jsonl` exceeds 4096 bytes (e.g., large `parameters` map), crash recovery will fail to detect the corruption. The truncation won't happen and subsequent writes will append after a malformed line.

**Recommendation:** Increase buffer or use `io.SeekEnd` with incremental backward scanning.

**BRD Ref:** SEC-15 (data integrity)

#### N-4: Audit retention uses file ModTime instead of filename date

`cleanupExpired` checks `info.ModTime()` but rotated files are named `audit-YYYY-MM-DD.jsonl`. A file touched by backup tools could be retained longer than intended; a file with wrong mtime could be deleted early. Parsing the date from the filename would be more deterministic.

**BRD Ref:** SEC-15

#### N-5: `strings.Title` deprecated in CheckDMSafety

`policy.go:198` uses `strings.Title` which is deprecated since Go 1.18. Should use `golang.org/x/text/cases.Title`.

**BRD Ref:** CLAUDE.md: Go 1.21+ target

#### N-6: Tamper-evident log chain (SEC-18) has no structural preparation

`AuditPolicy.TamperEvident` is parsed but unused. The `Entry` struct has no `hmac` or `prev_hash` field. Wave 3 will need to add fields to Entry — which is a schema change that could break log consumers.

**Recommendation:** Add `HMAC string `json:"hmac,omitempty"`` and `PrevHash string `json:"prev_hash,omitempty"`` to `Entry` now (always empty in Wave 2). This avoids a breaking schema change in Wave 3.

**BRD Ref:** SEC-18

## Consequences

### Positive
- Core abstractions (`SandboxBackend`, `Evaluator`, `Logger`, `SSRFChecker`, `SlidingWindow`) are well-designed and composable
- BRD requirement traceability is excellent — every test cites spec line numbers
- Immutable evaluator (SEC-12) is correct for static policies
- SSRF checker covers all RFC 1918/4193/6598 ranges plus cloud metadata
- Audit logger handles crash recovery, rotation, retention, and degraded mode
- E2e tests validate the full policy→audit pipeline

### Negative
- Seccomp is a stub — SEC-02 is not enforced (blocker)
- Code duplication between policy and security packages creates divergence risk
- No automatic audit logging means audit trail completeness depends on caller discipline
- FallbackBackend provides weaker security than documented

### Neutral
- Windows backend (Appendix A: Job Objects + Restricted Tokens) is not yet implemented — acceptable for Phase 1 (Linux-first) but must be tracked for Desktop variant
- Two-layer policy (SEC-10) deferred to Wave 3 — current single-layer is sufficient for Phase 1

## Affected Components

- **Backend:** `pkg/sandbox/` (seccomp fix), `pkg/policy/` (type unification, interceptor), `pkg/audit/` (schema prep), `pkg/security/` (dedup, SSRF URL parsing)
- **Frontend:** None
- **Variants:** All three variants affected by policy engine changes; sandbox changes are Linux-only (open source/SaaS) with fallback improvements helping Desktop (Electron on macOS/Windows)

## Resolution Status (as of 2026-04-10)

Most findings have been addressed across Waves 1-4. Two items are **partially
resolved** — the code for kernel-level enforcement exists and is tested in
isolation, but the runtime wiring that installs it on the Omnipus process is
still missing. These gaps are tracked as follow-up work; the sandbox status
endpoint (added in Wave 5) surfaces them to operators via a `policy_applied`
flag and an explanatory note.

**Blockers — Partially resolved**
- **B-1 (Seccomp BPF install)**: Assembly code landed in pre-wave prep.
  `pkg/sandbox/seccomp_linux.go` assembles a real BPF program via
  `unix.SockFilter`/`unix.SockFprog` and has an `Install()` method that calls
  `unix.Syscall(SYS_SECCOMP, ...)` with `SECCOMP_FILTER_FLAG_TSYNC`. **Gap:**
  `Install()` has no production callers — the seccomp filter is never
  installed on the running process. The status endpoint reports
  `seccomp_enabled: false` and `policy_applied: false` until this is wired.
  Follow-up: wire `SeccompProgram.Install()` into `LinuxBackend.Apply()` and
  call `Apply()` at agent loop startup.
- **B-2 (Duplicated glob/firstToken)**: Fully resolved in pre-wave prep.
  `execapproval.go` delegates to `policy.MatchGlob()` and `policy.FirstToken()`.

**Warnings — Resolved and partially resolved**
- **W-1 (FallbackBackend access-flag handling + ApplyToCmd no-op)**:
  *Partially resolved* in Wave 2. `ApplyToCmd` now injects
  `OMNIPUS_SANDBOX_MODE=fallback` and `OMNIPUS_SANDBOX_PATHS` env vars so
  cooperative child processes can self-enforce. **Gap:** `CheckPath` still
  ignores access flags (the comment explicitly says so); a new
  `CheckPathAccess` method was added as an opt-in API but has no production
  callers. Follow-up: migrate callers to `CheckPathAccess` or make
  `CheckPath` a fail-closed alias that requires all access flags.
- **W-2 (Duplicated SecurityConfig / AuditConfig / SSRFConfig / ExecConfig
  types)**: Resolved. The legacy `*ConfigFile`/`*Config` shadow types were
  removed; `pkg/policy/policy.go` now defines a single canonical
  `SecurityConfig` with nested `AuditPolicy`, `SSRFPolicy`, `ExecPolicy`,
  `FilesystemPolicy`, `RateLimitsPolicy`, `PromptGuardConfig`, and
  `SkillTrustPolicy` — each present once.
- **W-3 (No automatic audit logging from evaluator)**: Fixed in Wave 1. New
  `PolicyAuditor` wraps `Evaluator` and auto-logs every decision.
- **W-4 (Global cost cap RetryAfterSeconds: 0)**: Verified resolved —
  `ratelimit.go` computes `time.Until(nextMidnightUTC).Seconds()` correctly.
  Wave 4 additionally added `RecordSpend()` which fixes the related bug where
  `CheckGlobalCostCap` stalled the accumulator at cap-epsilon.

**Notes — Resolved or deferred**
- **N-1 (SEC-10 two-layer enforcement)**: Deferred to Phase 2 per BRD scope.
- **N-2 (extractHost hand-rolled parsing)**: Not addressed — works correctly
  for current SSRF use cases.
- **N-3 (audit readLastLine 4096-byte buffer)**: Not addressed — acceptable
  for current audit entry sizes.
- **N-4 (audit retention uses ModTime)**: Not addressed — acceptable for
  daily rotation.
- **N-5 (strings.Title deprecation)**: Not addressed — pre-existing,
  non-security-critical.
- **N-6 (Tamper-evident log chain prep)**: Deferred to Phase 2 per SEC-18 scope.

## Known gaps for follow-up

Two items from the resolution status above need runtime wiring before the
Wave 2 kernel enforcement layer is truly active:

1. **Wire `LinuxBackend.Apply()` at agent loop startup.** Currently the backend
   is selected and stored but `Apply()` is never called, so no Landlock
   restrictions are installed on the Omnipus process. Without this, the
   `ExecTool.ApplyToCmd` comment ("Landlock inherits to children natively")
   is correct in theory but vacuous in practice — there is nothing to inherit
   from. The sandbox status endpoint surfaces this as
   `policy_applied: false` with a note.
2. **Wire `SeccompProgram.Install()` into `LinuxBackend.Apply()`.** The BPF
   program is assembled but never installed. Until this is wired, seccomp
   syscall filtering is not active.

Both are pre-existing gaps that predate the Wave 1-4 security wiring work.
They were discovered during Wave 5's sandbox status UI review — the UI was
originally going to claim "seccomp enabled" unconditionally based on
capability, which the Wave 5 review correctly flagged as misleading.

**Status: Accepted (with known gaps)** — 2026-04-10. The policy engine,
audit logging, rate limiting, prompt guard, exec allowlist, and exec proxy
layers are in production. Kernel-level sandbox enforcement (Landlock +
seccomp) has the code landed but needs a runtime wiring step documented
above. The sandbox status endpoint honestly reports this state via
`policy_applied: false` rather than misrepresenting capability as enforcement.

## Wave 3 Readiness Assessment

| Wave 3 Feature | Current Readiness | Gap |
|---|---|---|
| Hot-reload (SEC-13) | Medium | Evaluator is immutable — need atomic swap of `*Evaluator` pointer. Feasible, no structural blockers. |
| RBAC (SEC-19) | Low | No role types, no role-to-capability mapping in SecurityConfig. Needs new fields and evaluator logic. |
| Tamper-evident logs (SEC-18) | Low | Entry struct lacks HMAC fields. Adding now (N-6) would raise to Medium. |
| Two-layer policy (SEC-10) | Medium | Single evaluator can be extended with sandbox-level pre-check. No structural blockers. |
