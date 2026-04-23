# Adversarial Review: Sprint K — Security settings UI/config parity + score-display correction

**Spec reviewed**: `docs/plan/sprint-k-security-ui-parity-spec.md`
**Review date**: 2026-04-22
**Verdict**: BLOCK

## Executive Summary

This spec attempts a wide UI/config-parity sprint on top of an already-in-flight PR #137. Review found 4 critical flaws that contradict the existing codebase (SSRF shape, retention semantics, user-token generation flow, allowed-paths R/W semantics). 7 major findings cover naming/ID inconsistencies, missing rollback paths, security-sensitive omissions, and inoperability gaps. If shipped as written, the sprint will either fail to compile (wrong config shape) or ship quietly broken semantics (retention, token rotation). Must revise the foundational assumptions before taskify.

| Severity | Count |
|----------|-------|
| CRITICAL | 4 |
| MAJOR | 9 |
| MINOR | 7 |
| OBSERVATION | 4 |
| **Total** | **24** |

---

## Findings

### CRITICAL Findings

#### [CRIT-001] SSRF config shape contradicts existing code — `allow_internal` is already a []string, not a bool

- **Lens**: Incorrectness / Inconsistency
- **Affected section**: US-7 (lines 183–202), FR-006 (line 813), clarification #7 (line 878)
- **Description**: The spec states *"The stored config keeps the existing shape: `sandbox.ssrf.allow_internal: bool` PLUS a new `sandbox.ssrf.allow_internal_cidrs: []string`"* (line 190). This is factually wrong. The current `OmnipusSSRFConfig` in `pkg/config/sandbox.go:90–101` defines:
  ```go
  type OmnipusSSRFConfig struct {
      Enabled       bool     `json:"enabled,omitempty"`
      AllowInternal []string `json:"allow_internal,omitempty"`  // already a list
  }
  ```
  `NewSSRFChecker([]string)` in `pkg/security/ssrf.go:73` already takes a heterogeneous list that accepts hostnames, exact IPs, and CIDRs in one slice (see doc comment lines 63–72). There is no bool today and no `allow_internal_cidrs` field is needed.
- **Impact**: Implementing the spec as written either (a) breaks backward compatibility by narrowing `allow_internal` from list → bool, (b) silently drops existing operator data on first save, or (c) introduces two overlapping fields with ambiguous precedence that the handler logic must reconcile. US-7 AC-5's "CIDR list is authoritative when non-empty" invents resolution rules the `SSRFChecker` does not know about.
- **Recommendation**: Delete the bool proposal. Either:
  - (a) Re-use the existing `AllowInternal []string` field directly. The UI's "simple toggle" is then sugar — when ON the list gets a single preset entry like `{"10.0.0.0/8","172.16.0.0/12","192.168.0.0/16","::1","127.0.0.0/8","fc00::/7"}`; when OFF the list is empty. Both modes share one storage field.
  - (b) Explicitly deprecate `allow_internal` and introduce a whole new shape with a migration path and version gate. Update FR-006, the SSRF allow_internal dataset, and every BDD scenario that references `allow_internal_cidrs`.
  Whichever path is chosen, update FR-006 line 813 to match the ACTUAL existing Go type.

---

#### [CRIT-002] `storage.retention.session_days = 0` semantic flip will silently invert operator intent

- **Lens**: Incorrectness / Insecurity (data loss)
- **Affected section**: US-9 (line 224), Scenario "Set retention to 0 (forever) with warning" (lines 576–585)
- **Description**: The spec states *"Default 90. 0 = keep forever."* (line 224). The existing code in `pkg/config/config.go:169–175` says the opposite:
  ```go
  func (r OmnipusRetentionConfig) RetentionSessionDays() int {
      if r.SessionDays <= 0 {
          return 90
      }
      return r.SessionDays
  }
  ```
  Today `SessionDays=0` means *"use default 90"*, not *"keep forever"*. An operator who set 0 years ago expecting default behaviour will, after this sprint, suddenly find their sessions accumulating indefinitely without any warning or migration.
- **Impact**: Disk-fill incident waiting to happen on upgrade for any deployment that has `session_days: 0` (including empty/unset, since Go zero-values map to 0 during JSON unmarshalling). Also reverses the UI warning — existing operators won't see the "retention disabled" banner because they'll be unaware their value flipped meaning.
- **Recommendation**: Either (a) keep the existing semantics and use a sentinel (e.g., `-1`) for "forever", or (b) explicitly spec a migration: on gateway boot, if `session_days == 0` and no explicit `session_days_disabled: true` flag is set, rewrite config to `session_days: 90`. The spec must call out the migration and add a regression BDD scenario covering pre-upgrade `session_days: 0` → post-upgrade still defaults to 90 unless the operator opts into "disabled" via a distinct field.

---

#### [CRIT-003] User creation cannot return a bearer token — the existing token model only mints tokens at login

- **Lens**: Incorrectness / Infeasibility
- **Affected section**: US-10 AC-1 (line 255), FR-009 (line 816), FR-016 (line 823), Scenario "Create user returns token exactly once" (lines 590–598)
- **Description**: The spec says POST `/api/v1/users` *"returns the bearer token exactly once (token never returned again)"* and that *"Bearer tokens for newly-created users MUST be cryptographically random (≥32 bytes) and persisted only as bcrypt hashes."* But the current auth flow in `pkg/gateway/rest_auth.go:267–382` mints a token only when the user presents valid username+password at `POST /api/v1/auth/login`. `HandleLogin` regenerates a fresh token and overwrites `TokenHash` every login. Every user carries at most one `TokenHash` at a time.
  - If user-creation also writes a `TokenHash`, the first login will overwrite it, invalidating the token the admin just handed to Alice.
  - The spec's scenario says the response returns `token: '<64-char-hex>'` but the existing token format from `generateUserToken` in `pkg/gateway/rest_auth.go:643–649` is `"omnipus_" + hex.EncodeToString(bytes)` = 72 characters, not 64-char hex.
- **Impact**: Alice copies the "one-time token" from the modal, then is told to log in with her password, at which point her token changes. Either the modal lies or the login flow must be special-cased. Also, the 64-char-hex claim will fail any regex-based assertion in tests.
- **Recommendation**: Pick one model and spec it completely:
  - (a) *Admin creates with password, user logs in to get token* — POST returns only `{username, role}` and the admin hands the user their **password**. Drop the one-time-token-on-creation scenario entirely.
  - (b) *Admin creates with token* — POST accepts no password and returns the one-time token; the login flow is bypassed for that user (token auth only). The spec must then state how password reset and change-password apply to token-only users.
  - (c) *Both* — POST sets both `password_hash` and `token_hash`, and login is changed to NOT overwrite the stored token_hash if it was set by user-creation (tokens live until rotation). This is the most invasive — spec the new bcrypt-compare rules for coexisting password+token paths.
  Also fix the literal format claim: either use `omnipus_<64-hex>` to match `generateUserToken`, or rebuild the generator and spec the new format.

---

#### [CRIT-004] "AllowedPaths grants read+write" is wrong — existing FR-J-013 makes them read-only by default

- **Lens**: Incorrectness / Insecurity
- **Affected section**: US-6 AC-5 (line 179), US-6 scenario "System-restricted path override" (lines 533–540), Test Dataset row 5 (line 762)
- **Description**: US-6 AC-5 says *"the behavior stays per FR-J-013: user-read wins, write stripped"*, implying admin-added paths grant RW by default and `/etc` overlap simply strips the W bit. The dataset row for `/etc/foo` is labelled "valid but flagged" with a note "Write access to system-restricted paths is always denied" — suggesting non-`/etc` paths would grant write. This contradicts `pkg/config/sandbox.go:47–49`:
  ```go
  // AllowedPaths lists additional filesystem paths the sandbox may read.
  // Paths outside this list (and the agent workspace) are inaccessible.
  AllowedPaths []string `json:"allowed_paths,omitempty"`
  ```
  `AllowedPaths` is read-only by design. The `/etc`-specific write-stripping rule in FR-J-013 is about **agent workspace** paths that overlap `/etc`, not about `AllowedPaths` entries. The spec's framing implies operators can grant write access to arbitrary paths via the UI — which would be a sandbox escape vector.
- **Impact**: An operator reads the UI, adds `/var/myapp/data`, expects the agent to be able to write there, and files a bug when the agent can only read. Worse: if a future implementer takes the spec literally and grants write on AllowedPaths, they create a sandbox escape — agent gains write access to any arbitrary user-chosen path.
- **Recommendation**: Rewrite US-6 AC-5 and the BDD scenario to state explicitly: *"AllowedPaths entries grant READ-ONLY access. The row displays a read-only badge. Write access to any non-workspace path is never available via this editor (by design — write-capable paths are sandbox policy, not settings)."* Remove the "write stripped" language from this story — that's sprint-J's FR-J-013 about workspace overlap, not Sprint K's editor.

---

### MAJOR Findings

#### [MAJ-001] BDD scenario references wrong issue number (#103 vs #138)

- **Lens**: Inconsistency
- **Affected section**: Scenario "Kernel 6.8 enforce save gets warning" (line 661) vs US-12 (lines 282–303)
- **Description**: US-12 and FR-014 reference *"issue #138"* consistently (7 occurrences). The BDD scenario at line 661 asserts text matching `"known incompatibility.*issue #103"`. Either the tracked issue is #138 and the BDD regex is wrong, or vice versa.
- **Impact**: The test will fail as soon as implementation matches the prose (or vice versa). A reviewer blindly trusting the BDD scenario will lock the wrong issue ID into production UI copy.
- **Recommendation**: Update the BDD scenario regex to `"known incompatibility.*issue #138"` and update the matching UI copy string in US-12 Surface-3 paragraph to the same literal text. Add a single-source-of-truth constant in the implementation like `const LandlockABI4IssueRef = "#138"` and reference it everywhere.

---

#### [MAJ-002] `session.dm_scope` UI surfaces only 2 of 4 valid values, and uses `global` which is not a valid scope

- **Lens**: Incorrectness / Incompleteness
- **Affected section**: US-8 (lines 205–215), FR-007 (line 814)
- **Description**: US-8 says the UI offers a radio group "between `global` and `per-channel-peer`". But `pkg/routing/session_key.go:12–15` defines four scopes: `main`, `per-peer`, `per-channel-peer`, `per-account-channel-peer`. There is no `global`. FR-007 correctly says the endpoint accepts all four values. The UI spec and the FR disagree, and the UI spec uses a name that doesn't exist in code.
- **Impact**: (a) Operators lose access to two of four valid scopes through the UI. (b) Whichever value the UI sends for "global" (probably `main`) will silently be saved but the UI label won't round-trip — GET returns `main`, UI can't find `main` in its `global/per-channel-peer` radio group, so the form renders in a broken state with nothing selected. (c) Test scenario "Change scope" doesn't specify what value is saved.
- **Recommendation**: Expand the radio group to all four values from `session_key.go`, using the canonical keys: `main`, `per-peer`, `per-channel-peer`, `per-account-channel-peer`. Add human-readable subtitles in the UI for each. Remove `global` from the spec entirely.

---

#### [MAJ-003] "Reset password invalidates prior bearer" has no implementation contract

- **Lens**: Incompleteness / Ambiguity
- **Affected section**: US-10 AC-3 (line 258), FR-009 (line 816)
- **Description**: The spec says admin-triggered password reset *"invalidates the target user's existing bearer token"*. But the existing model stores `TokenHash` separately from `PasswordHash`. Just changing `PasswordHash` does not invalidate the token (the middleware at `pkg/gateway/rest_auth.go:242` checks only `TokenHash`). The spec does not say whether reset clears `TokenHash`, sets it to a random value, or tracks a `token_version` counter.
- **Impact**: A well-meaning implementer who only rewrites `PasswordHash` leaves the old token valid — Alice's compromised token keeps working after the admin reset her password. This breaks the stated security property.
- **Recommendation**: Make the mechanism explicit in FR-009: *"Admin-triggered password reset MUST also zero `TokenHash` in the same `safeUpdateConfigJSON` transaction. The next successful login for that user mints a fresh token. Any request bearing the pre-reset token receives 401 `invalid credentials`."* Add a BDD assertion that hits the middleware directly with the pre-reset token after the reset has landed and expects 401 (not just 401 on a login attempt).

---

#### [MAJ-004] No spec for what `blockedKeys` change actually looks like — nested paths aren't supported today

- **Lens**: Infeasibility / Ambiguity
- **Affected section**: Existing Codebase Context table line 48, plus clarification "add 'gateway.users' to the block list"
- **Description**: The spec claims `blockedKeys` (at `pkg/gateway/rest.go:1622`) gains `gateway.users`. But the existing `blocked` map is a flat `map[string]bool{"sandbox": true, "credentials": true, "security": true}` that matches only top-level keys. There's no dot-path traversal. Adding `"gateway.users"` to that map does nothing — the handler iterates top-level keys from the PUT body.
- **Impact**: A non-admin or admin using the generic `PUT /config` endpoint can still overwrite `gateway.users` wholesale, bypassing the new dedicated user endpoints entirely. This is a privilege-escalation vector: an attacker who gets any admin's token could POST `{"gateway": {"users": [...new admin list...]}}` and win the whole deployment.
- **Recommendation**: Rewrite the Codebase Context note to either (a) spec a new nested-path checker (`blockedPaths []string{"gateway.users"}` with a dotted-path walker) OR (b) block the whole `gateway` key from generic PUT and require every gateway setting to use dedicated endpoints. Pick one and write a regression test: `TestConfigPUT_CannotSetGatewayUsers_Returns403` that covers both a whole-gateway replacement and a nested-users replacement.

---

#### [MAJ-005] No spec for concurrent edits in user CRUD — race conditions on last-admin guard

- **Lens**: Incompleteness / Infeasibility
- **Affected section**: Edge Cases section (line 329), US-10, FR-015
- **Description**: The spec says concurrent edits are "acceptable because admin set is small". But the last-admin guard (FR-015) is time-of-check-to-time-of-use vulnerable if two admins simultaneously demote/delete each other. Alice and Bob are both admins. Alice demotes Bob at T0; Bob demotes Alice at T0+1ms. If both requests pass the "is there another admin" check before either is committed, both end up non-admin and the deployment is locked out. `safeUpdateConfigJSON` holds `configMu` during the mutation but the read-modify-write cycle inside the callback is where the race sits if both callbacks check `len(admins) > 1` against the same pre-commit state.
- **Impact**: The "last-admin" guard is easily defeated by racing requests. Operators can accidentally brick their own deployment. Recovery requires direct `config.json` editing, which the whole sprint is trying to eliminate.
- **Recommendation**: Add a BDD scenario explicitly for the race: "Two admins concurrently demote each other → exactly one succeeds, the other returns 409." State that the guard MUST be evaluated inside the `safeUpdateConfigJSON` callback (post-configMu-acquire) using the just-read JSON map, NOT against a stale snapshot. Add an integration test that fires 2 concurrent demote requests in parallel goroutines.

---

#### [MAJ-006] `dev_mode_bypass` explicit non-behavior does not cover the bypass side-effects

- **Lens**: Insecurity / Incompleteness
- **Affected section**: Explicit Non-Behaviors (line 342)
- **Description**: The spec says the UI must not expose `dev_mode_bypass`. Good — but it does not say what the new endpoints MUST do when `dev_mode_bypass=true`. Today with bypass on, every request is authenticated-as-admin. The new admin-only endpoints `PATCH /users/{u}/role`, `DELETE /users/{u}`, password resets, etc. would become *anonymously* reachable. Anyone with network access could delete the admin account or reset any password.
- **Impact**: Shipping these endpoints unmodified on a dev-mode deployment hands full deployment control to any network-connected attacker. CLAUDE.md actually calls out using `dev_mode_bypass: true` during onboarding (line E2E Testing section 2), so dev deployments with bypass enabled are non-trivial populations.
- **Recommendation**: Add an FR: *"When `gateway.dev_mode_bypass=true`, the new user-management endpoints (POST /users, PATCH /users/:u/role, DELETE /users/:u, PUT /users/:u/password, PUT /users/self/password, and `/api/v1/config/pending-restart` with write semantics) MUST return HTTP 503 with body `{error: 'user management disabled in dev-mode-bypass'}`. The UI MUST hide the Access tab when `GET /api/v1/config` reveals dev-mode-bypass is on."* Add a BDD scenario and integration test.

---

#### [MAJ-007] `PUT /api/v1/users/self/password` collides with existing `POST /api/v1/auth/change-password`

- **Lens**: Inconsistency
- **Affected section**: FR-009 (line 816), test dataset rows 9–10 (lines 788–789)
- **Description**: The existing `HandleChangePassword` at `pkg/gateway/rest_auth.go:561–640` is registered at `POST /api/v1/auth/change-password` (verb POST, path under `/auth/`). The spec proposes `PUT /api/v1/users/self/password` — different verb, different path. Neither the spec nor the traceability matrix addresses deduplication: do they co-exist? Does the spec deprecate the old one? Does the SPA already use the old one?
- **Impact**: Two endpoints doing the same thing with different CSRF/auth wiring leads to confusion, duplicated bugs, and stale frontend code that uses the old endpoint while new code uses the new one.
- **Recommendation**: Either (a) replace the existing `POST /api/v1/auth/change-password` with `PUT /api/v1/users/self/password` — add a deprecation note, remove the old handler, rewrite `src/components/settings/ProfileSection.tsx` or wherever change-password is wired. Or (b) reuse the existing endpoint and remove `PUT /users/self/password` from FR-009. Either way, the spec must mention the collision and pick one.

---

#### [MAJ-008] No rollback path for restart-required settings

- **Lens**: Inoperability / Incompleteness
- **Affected section**: US-11, FR-010, FR-011
- **Description**: The "pending restart" banner shows queued changes until the gateway restarts. There's no spec for *undoing* a queued change before restart. An admin clicks "save enforce" by accident, sees the banner, and wants to revert to "off" without restarting first. The spec says nothing — the admin either has to actually restart (the change they don't want), or edit the setting again to "off" (which would hot-reload to applied=off=persisted but still leave the banner entry if the applied state was already off... actually the diff logic would clear it, but the spec doesn't assert this).
- **Impact**: Confusion and broken restart-required UX. Also breaks the Banner scenario #3 (line 275) implicitly — the spec doesn't explain what happens if I save `enforce`, then save back to `off`, before restart. Is the banner cleared? Is there still a "pending revert" row?
- **Recommendation**: Add an FR and a BDD scenario: *"Given the banner lists a pending change X→Y, When the admin saves Y→X (back to original applied value), Then the row disappears from the banner and `/api/v1/config/pending-restart` returns an empty list without requiring restart."* State that `pending-restart` is computed as diff (`persisted != applied`), not as history, so set-then-revert cancels.

---

#### [MAJ-009] `abi_version` field is int-omitempty, not nullable — UI claim of `abi_version: null` is wrong

- **Lens**: Incorrectness
- **Affected section**: Integration Boundaries: Landlock kernel ABI probe (lines 374–379), FR-014
- **Description**: The spec says *"On failure: abi_version: null → UI assumes no ABI gap"*. But `pkg/sandbox/sandbox.go:523` defines `ABIVersion int json:"abi_version,omitempty"`. An int-omitempty field serialises as ABSENT (not `null`) when it equals 0. On non-Linux systems, the field doesn't appear at all.
- **Impact**: A UI check of `abi_version === null` will never match — the field is simply missing from the response JSON. The ABI-v4 banner will fire correctly because `undefined >= 4` is `false`, but any code path that depends on explicit null-checking will be wrong.
- **Recommendation**: Restate the integration boundary: *"On non-Linux or failed-probe systems, `abi_version` is omitted from the response. The UI treats `typeof response.abi_version !== 'number'` as 'no probe available' and suppresses the ABI v4 warning. Add a BDD scenario: `Given /sandbox-status returns no abi_version field, When SandboxSection renders, Then the ABI v4 banner is not visible.`"* Also clarify in FR-014: the boot-log rule fires only when `abi_version >= 4` **and** the field is present; a missing field does not trigger the log.

---

### MINOR Findings

#### [MIN-001] `skill_trust` dataset row 4 (`BLOCK_UNVERIFIED`, case-variant) specifies normalization not stated in FR

- **Lens**: Ambiguity
- **Affected section**: Skill trust dataset row 4 (line 738), FR-003 (line 810)
- **Description**: The dataset asserts uppercased input is normalized to lowercase with 200 status, but FR-003 says "MUST reject any other value with 400" without mentioning normalization. These conflict — is uppercase accepted or rejected?
- **Recommendation**: Decide: either reject non-canonical casing with 400, or normalize explicitly in FR-003 ("MUST accept case-insensitively and normalize to lowercase before persisting").

---

#### [MIN-002] Retention sweep goroutine lifecycle not specified

- **Lens**: Incompleteness / Inoperability
- **Affected section**: US-9 backend deliverables (line 228)
- **Description**: The spec mandates "a daily goroutine at gateway boot" but does not address: (a) how it's stopped on graceful shutdown, (b) what happens if the sweep is in-progress when config reloads, (c) whether concurrent sweeps (on-demand + nightly) are allowed, (d) how failures are surfaced (metric? alert? audit log?).
- **Recommendation**: Add: *"A single `*time.Ticker` goroutine runs from gateway start to shutdown. On gateway shutdown, the goroutine observes a context cancellation and exits cleanly. Concurrent sweeps (e.g., admin triggers POST while nightly fires) are serialised by a `sync.Mutex`; the second caller receives 409 'sweep in progress'. Sweep errors are logged via `slog.Error` with event=retention_sweep_failed and do not abort subsequent scheduled runs."*

---

#### [MIN-003] No spec for audit logging of admin-only security-setting mutations

- **Lens**: Insecurity (Repudiation)
- **Affected section**: FR-002 through FR-009
- **Description**: Every new admin endpoint mutates security-sensitive state. No FR requires the mutation to be written to the audit log. A malicious/compromised admin could change `skill_trust = allow_all`, exploit, then reset it — leaving no forensic trail.
- **Recommendation**: Add FR-018: *"Every state-changing admin endpoint in Sprint K MUST emit an audit-log entry via the existing `sandbox.audit_log` facility (when enabled) with `event=security_setting_change`, actor username, resource key, old value, new value, timestamp. Applies to audit-log, skill-trust, prompt-guard, rate-limits, sandbox-config, session-scope, retention, user CRUD, password reset, role change."*

---

#### [MIN-004] Rate-limit dataset doesn't cover non-integer / NaN / string-number cases

- **Lens**: Incompleteness
- **Affected section**: Rate-limits dataset (lines 742–752)
- **Description**: Only integer/float positive and -5 negative covered. Missing: string `"50"` (JSON would accept as string), NaN, Infinity, float fraction for non-cost fields (can `max_agent_llm_calls_per_hour` be 10.5?), SQL-injection-looking strings, values exceeding int64.
- **Recommendation**: Add rows for non-integer in integer-typed fields (should 400), JSON string-number, NaN/Inf (400), values > MaxInt32 (still 200 if int64 allowed).

---

#### [MIN-005] "Copy token" modal relies on localStorage/clipboard APIs with no fallback

- **Lens**: Inoperability
- **Affected section**: US-10 AC-1, US-12 UI banner
- **Description**: The spec doesn't address browsers/contexts where clipboard API is unavailable (e.g., http:// non-localhost, Firefox strict privacy mode). Admin clicks "Copy" → nothing happens → token lost.
- **Recommendation**: State the modal MUST also expose the raw token in a selectable `<input readonly>` or `<textarea>` as a fallback for clipboard API failure, and MUST detect clipboard-unavailable and fall back explicitly.

---

#### [MIN-006] Traceability matrix missing FR-018-equivalent for self-change-password scenario

- **Lens**: Incompleteness
- **Affected section**: Traceability matrix FR-009 row (line 854)
- **Description**: The matrix FR-009 line mentions `TestHandleSelfChangePassword_RequiresCurrent` but this test is NOT in the TDD plan (Order 1–28, lines 687–714). There's no test row 29 or later.
- **Recommendation**: Add a test row for `TestHandleSelfChangePassword_RequiresCurrent` and `TestHandleSelfChangePassword_WrongCurrentReturns401` to the TDD plan.

---

#### [MIN-007] Spec mentions "PromptGuardSection already exists" but the file is read-only — no mention of refactor path

- **Lens**: Ambiguity
- **Affected section**: US-4 (line 141), Symbols Involved table
- **Description**: The symbols table says `PromptGuardSection.tsx` is "not listed" in **modifies**. US-4 says *"promote it to editable"* but the table doesn't list the file.
- **Recommendation**: Add `src/components/settings/PromptGuardSection.tsx` as **modifies** in the Symbols Involved table.

---

### Observations

#### [OBS-001] Spec would benefit from an explicit interaction diagram

- **Lens**: Ambiguity
- **Affected section**: Decisions / Integration Boundaries
- **Suggestion**: Given 10 new endpoints + 4 modified components + 2 new zustand stores + a banner + 3 Landlock surfaces, a sequence diagram for one representative flow (save → banner → restart → banner clears) would reduce ambiguity.

---

#### [OBS-002] `RestartBanner` coordination with other banners (e.g., ABI v4) is not specified

- **Lens**: Inoperability
- **Affected section**: US-11, US-12
- **Suggestion**: Clarify stacking order when both `RestartBanner` and the ABI v4 banner are visible. Also what happens on mobile viewport — do they stack vertically?

---

#### [OBS-003] No mention of accessibility (ARIA, keyboard nav) for new components

- **Lens**: Incompleteness
- **Affected section**: All new sections (RateLimits, Retention, Users, SessionRouting, RestartBanner)
- **Suggestion**: Existing `SandboxSection` has ARIA patterns. State that new components inherit the same accessibility level — keyboard-navigable, focus-visible, `aria-disabled` on read-only forms, live regions for banner updates.

---

#### [OBS-004] Consider i18n copy extraction

- **Lens**: Incompleteness
- **Affected section**: Every `--color-error` / `--color-warning` string literal
- **Suggestion**: Out of scope for this sprint, but noting: the spec hard-codes English copy in every scenario ("Cannot delete yourself while you are the only admin"). A future i18n pass will require rewiring these — might be worth an FR to extract to a messages file now while edits are localized.

---

## Structural Integrity

### Variant A: Plan-Spec Format

| Check | Result | Notes |
|-------|--------|-------|
| Every user story has acceptance scenarios | PASS | All 12 US have AC blocks |
| Every acceptance scenario has BDD scenarios | PARTIAL | US-10 AC-6 (admin sees "Change my password" not "Reset password" on own row) has no corresponding BDD scenario |
| Every BDD scenario has `Traces to:` reference | PASS | All BDD blocks carry `Traces to:` |
| Every BDD scenario has a test in TDD plan | FAIL | "System-restricted path override" (line 533) has no TDD test row. `TestHandleSelfChangePassword_RequiresCurrent` (referenced in matrix) has no TDD row. `TestLandlockABIv4_BootLogOnce`, `TestSandboxSection_ABIv4Banner`, `TestSandboxSection_ABIv3NoBanner` (matrix FR-014) have no TDD rows. `TestRetentionSweep_DeletesAgedFiles`, `TestRetentionSweep_NightlyGoroutineTicks` (matrix FR-008) have no TDD rows |
| Every FR appears in traceability matrix | PASS | FR-001 through FR-017 all listed |
| Every BDD scenario in traceability matrix | PARTIAL | "System-restricted path override" and "Hot-reload save leaves banner untouched" are not individually listed |
| Test datasets cover boundaries/edges/errors | PARTIAL | Good coverage on happy + basic negatives. Missing: concurrency (last-admin race), CSRF-without-cookie, very-large-integer overflow, IDN/unicode usernames |
| Regression impact addressed | PASS | Regression Test Requirements table is present, correctly flags `DiagnosticsSection.test.tsx` as needing rewrite |
| Success criteria are measurable | PARTIAL | SC-002 uses "≤ 3 clicks per setting" (subjective — depends on what counts as a click). SC-004 and SC-005 use concrete latency targets — good. SC-001 "200ms of `/doctor` response" is fine. SC-009 "100% of new BDD scenarios" circular. |

---

## Test Coverage Assessment

### Missing Test Categories

| Category | Gap Description | Affected Scenarios |
|----------|----------------|-------------------|
| Concurrency | No race-condition test for last-admin guard (two demotes in parallel). No test for simultaneous `/users` PUT and DELETE of the same user | US-10 |
| Audit logging | No test asserts that state-changing endpoints write to `sandbox.audit_log` when enabled | All FR-002..009 (MIN-003) |
| Session lifecycle | No test for what happens if admin's own token is invalidated by a concurrent admin (does the in-flight request 401?) | US-10 AC-3 |
| Boot-time failure | No test for retention goroutine panic recovery / ticker restart | FR-008 |
| CSRF with missing cookie | `TestCSRF_AllNewEndpointsSubjectToGate` is asserted as table-driven but no dataset rows show what happens when cookie is present but mismatched vs absent | FR-013 |
| Generic-PUT-on-blocked-path | No test that `PUT /config {"gateway": {"users": [...]}}` is rejected | MAJ-004 |
| Dev-mode-bypass interaction | No test for new endpoints under bypass=true | MAJ-006 |

### Dataset Gaps

| Dataset | Missing Boundary Type | Recommendation |
|---------|----------------------|----------------|
| SSRF allow_internal | IPv6 link-local (fe80::/10) | Add row 7: `fe80::1` — currently ambiguous whether it's allowed |
| User management | Username with spaces/unicode/`/` | Add row: `{username: "alice bob"}` → 400 invalid username |
| User management | Role case-variants | Add row: `{role: "ADMIN"}` — accepted as `admin` or rejected? |
| Allowed paths | Symlink detected by final-component check | Dataset covers rejection criteria but no row for a symlink path — implementation must `lstat` |
| Rate limits | String-number `"50"` | JSON tolerant decoders may accept this — spec behavior |
| Retention | Negative session_days | Dataset row missing — `{session_days: -1}` should 400 |

---

## STRIDE Threat Summary

| Component | S | T | R | I | D | E | Notes |
|-----------|---|---|---|---|---|---|-------|
| User CRUD endpoints | ok | ok | **risk** | ok | ok | **risk** | No audit log (MIN-003). Dev-mode-bypass grants elevation to anonymous (MAJ-006). Race on last-admin guard (MAJ-005) |
| Token model | **risk** | ok | ok | ok | ok | **risk** | Reset-password doesn't clear TokenHash explicitly (MAJ-003) |
| SSRF allow_internal | ok | ok | ok | ok | ok | **risk** | Wrong config shape (CRIT-001) could silently widen allowlist |
| Allowed paths editor | ok | ok | ok | ok | ok | **risk** | R/W semantics mis-spec'd — risk of granting agent write access (CRIT-004) |
| blockedKeys config gate | ok | ok | ok | ok | ok | **risk** | Nested-path bypass (MAJ-004) |
| RestartBanner | ok | ok | ok | **risk** | ok | ok | `pending-restart` surface discloses queued-change details — must be admin-only (clarification #5 confirms but no FR) |
| Retention sweep | ok | ok | ok | ok | **risk** | ok | No concurrency control between nightly + on-demand (MIN-002) |
| Doctor score display | ok | ok | ok | ok | ok | ok | Display-only fix |

**Legend**: risk = identified threat not mitigated in spec, ok = adequately addressed or not applicable

---

## Unasked Questions

1. What is the authoritative source-of-truth for the current SSRF allowlist — the existing `sandbox.ssrf.allow_internal []string` or the new bool+CIDR pair? (CRIT-001)
2. What happens to operators whose `storage.retention.session_days` is currently 0? (CRIT-002)
3. Does POST `/api/v1/users` return a token that survives login, or does login overwrite it? (CRIT-003)
4. Do admin-added `allowed_paths` grant read-write or read-only? (CRIT-004)
5. How is `TokenHash` cleared when an admin resets another user's password? (MAJ-003)
6. Which endpoint wins when `POST /api/v1/auth/change-password` and `PUT /api/v1/users/self/password` both exist? (MAJ-007)
7. What prevents two concurrent admin demotions from dropping the admin count to zero? (MAJ-005)
8. Do the new endpoints enforce auth when `dev_mode_bypass=true`, or do they anon-bypass? (MAJ-006)
9. What does the banner look like when a restart-required change is reverted before restart? (MAJ-008)
10. Is the audit log required to record these mutations? (MIN-003)
11. What happens if the SSRF `allow_internal` contains `0.0.0.0/0`? (spec allows CIDRs but no policy on wildcards)
12. If two admins edit the same setting in different tabs and save near-simultaneously, which save wins — and does the loser see a stale value until they reload? (Spec says last-write-wins but doesn't say the UI detects the stale-state.)
13. Is there a scenario for the bearer-token revocation flow when admin rotates their OWN token? (The edge cases section hints at it but no BDD.)

---

## Verdict Rationale

The spec is ambitious (12 stories, 17 FRs, 9 UI gaps, 1 significant backend addition) and mostly well-structured, but it contains four factual errors about the existing codebase that would either break compilation (SSRF shape, CRIT-001), silently invert data semantics (retention 0-means-forever, CRIT-002), leak tokens / break login (user creation model, CRIT-003), or introduce sandbox escapes (allowed-paths R/W confusion, CRIT-004). These aren't stylistic issues; each one is grounded in a specific quoted line from `pkg/config/sandbox.go`, `pkg/config/config.go`, or `pkg/gateway/rest_auth.go` that disagrees with the spec's claim.

The spec should be REVISED with the four CRITICAL findings resolved BEFORE any taskify or implementation. Major findings MAJ-001..009 should also be resolved in the same revision pass — several (MAJ-004, MAJ-006) are security-sensitive and would need to be re-introduced later as bug-fix work if they slipped through. Minor and observation-level findings can be addressed inline during implementation but are flagged for the spec author's convenience.

### Recommended Next Actions

- [ ] Resolve CRIT-001 by aligning the SSRF config shape with the actual `OmnipusSSRFConfig` in `pkg/config/sandbox.go:90–101`.
- [ ] Resolve CRIT-002 by specifying the `session_days: 0` migration or using a sentinel value for "keep forever".
- [ ] Resolve CRIT-003 by picking one token-issuance model (create-time, login-time, or both) and writing the flow end-to-end, including the `omnipus_<hex>` format.
- [ ] Resolve CRIT-004 by explicitly stating AllowedPaths entries are read-only, removing the "write stripped" language.
- [ ] Resolve MAJ-001 by reconciling issue #103/#138 and using a single constant throughout.
- [ ] Resolve MAJ-002 by listing all four `DMScope` values in the UI spec and removing `global`.
- [ ] Resolve MAJ-003 by specifying that password reset clears `TokenHash`.
- [ ] Resolve MAJ-004 by choosing a `blockedKeys` strategy (nested-path matcher or block whole `gateway`) and adding a regression test.
- [ ] Resolve MAJ-005 by specifying that last-admin guard evaluates inside the `safeUpdateConfigJSON` callback and adding a parallel-request BDD scenario.
- [ ] Resolve MAJ-006 by adding FR-018 for `dev_mode_bypass` interaction with user-management endpoints.
- [ ] Resolve MAJ-007 by deprecating the old `/auth/change-password` or dropping the new `/users/self/password`.
- [ ] Resolve MAJ-008 by stating pending-restart is diff-based and set-then-revert clears the banner.
- [ ] Resolve MAJ-009 by re-wording the `abi_version` null-handling to omitempty/undefined handling.
- [ ] Add test rows for missing TDD entries flagged under "Every BDD scenario has a test in TDD plan".
- [ ] Add the concurrency, audit-log, and dev-mode-bypass test datasets.
