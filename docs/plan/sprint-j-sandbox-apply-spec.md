# Feature Specification: Sandbox Apply/Install Wiring at Gateway Boot

**Created**: 2026-04-20
**Status**: Draft
**Input**: GitHub issue #76 — "Landlock Apply() and seccomp Install() are never called at runtime"
**Branch**: `sprint-j-security-hardening`
**Depends on**: `docs/plan/wave2-security-layer-spec.md` (Sandbox backends, SEC-01/02/03)

---

## 1. Context

### Problem Statement

The Omnipus agentic core advertises kernel-level sandboxing (Landlock + seccomp, SEC-01/02/03) but the parent gateway process never actually calls `SandboxBackend.Apply()` or `SeccompProgram.Install()` at boot. Children spawned via `ApplyToCmd()` are sandboxed, but the gateway itself — the long-lived process that holds credentials, config, and agent state — runs unconfined.

The sandbox package itself is aware of the gap: `pkg/sandbox/sandbox.go:392-395` explicitly logs a status note when queried:

> "sandbox backend is capable of kernel-level enforcement but Apply() has not been called on the Omnipus process; child processes are not currently restricted by Landlock or seccomp"

### Intended Outcome

After this sprint:
1. On Linux ≥ 5.13 with `gateway.sandbox.enabled = true`, the gateway calls `Apply()` and then `Install()` once during boot, BEFORE serving requests.
2. On unsupported kernels/OSes, the gateway selects `FallbackBackend`, logs the degradation, and continues (graceful fallback per CLAUDE.md Hard Constraint #4).
3. A CLI override (`--sandbox=off`) and config key (`gateway.sandbox.enabled = false`) exist for local debugging.
4. The status endpoint reports `sandbox.applied = true` post-boot (instead of the current warning note).
5. Failures during apply/install on a kernel that claims support are **fatal** — the gateway fails closed rather than silently serving with no sandbox.

---

## 2. Phase 1 — Discovery & Requirements (Condensed)

| Dimension | Summary |
|-----------|---------|
| **Primary actor** | Operator booting the Omnipus gateway (`./omnipus gateway`) |
| **Secondary actor** | Developer running the gateway locally with a debugger attached |
| **Problem scope** | Parent-process sandbox enforcement only. Child-process sandboxing (`ApplyToCmd`) already works and is out of scope. |
| **Constraints** | CLAUDE.md: pure Go, ≤10MB RAM, graceful degradation, deny-by-default for security. No new runtime dependencies. |
| **Integration surface** | `pkg/gateway/gateway.go` boot path, `pkg/agent/loop.go` backend selection, `pkg/sandbox/*` enforcement, `pkg/config/sandbox.go` config, gateway CLI flags. |
| **Non-scope** | Windows Job Objects (stays as FallbackBackend on Windows until a later sprint); rewriting existing `pkg/sandbox` tests; changing the `SandboxBackend` interface. |

---

## 3. Phase 1.5 — Existing Codebase Context

### Symbols Involved

| Symbol | File:Line | Role |
|--------|-----------|------|
| `SandboxBackend` interface | `pkg/sandbox/sandbox.go:40-46` | Defines `Apply()` (parent) and `ApplyToCmd()` (children). No change needed. |
| `SelectBackend()` | `pkg/sandbox/sandbox.go:402-404` | Returns highest-capability backend available. Never fails. |
| `LinuxBackend.Apply()` | `pkg/sandbox/sandbox_linux.go:126` | Kernel Landlock enforcement on parent. Already implemented and tested. |
| `LinuxBackend.PolicyApplied()` | `pkg/sandbox/sandbox_linux.go:121-123` | Reports whether Apply() ran. Reuse; no change. |
| `SeccompProgram.Install()` | `pkg/sandbox/seccomp_linux.go:56` | Loads BPF filter into kernel. Already implemented. Called in tests only. |
| `BuildSeccompProgram()` | `pkg/sandbox/seccomp_linux.go:118` (approx) | Assembles the BPF program. Reuse; no change. |
| `FallbackBackend` | `pkg/sandbox/sandbox_fallback.go` | No-op enforcement; `Apply()` returns nil. |
| `SandboxStatus{}.PolicyApplied` | `pkg/sandbox/sandbox.go:386-396` | Already surfaces the gap in status output. Should flip to true after wiring. |
| `NewAgentLoop()` | `pkg/agent/loop.go:327-331` | Selects backend but does not `Apply()` it. |
| `Gateway.Start()` (implicit) | `pkg/gateway/gateway.go:334` | Constructs `AgentLoop`; should also orchestrate apply/install. |
| `OmnipusSandboxConfig.Enabled` | `pkg/config/sandbox.go:28-31` | Existing feature toggle; repurpose for the "on/off" decision. |
| `computeWorkspacePolicy` | **NEW** — to be added | Builds a `SandboxPolicy` with `$OMNIPUS_HOME/**` writable, minimal system paths readable. |

### Impact Assessment

| Symbol Modified | Risk Level | d=1 Dependents | d=2 Dependents (notable) |
|-----------------|------------|----------------|--------------------------|
| Gateway boot path | HIGH | `cmd/omnipus/main.go`, integration tests in `pkg/gateway/rest_*_test.go` | All E2E flows (onboarding, chat, agents) |
| `NewAgentLoop` | LOW | 18 call sites (mostly `pkg/gateway/*_test.go`; see list below) | Test-only — production caller is `gateway.go:334` |
| `SandboxBackend` interface | NONE (unchanged) | All backends, all `ApplyToCmd` callers | n/a |
| `LinuxBackend.Apply()` | LOW | sprint-j integration test (new); existing subprocess test at `tests/security/sandbox_enforcement_linux_test.go:175` | n/a |
| `OmnipusSandboxConfig` | LOW | config load/save; doctor command | Any future sandbox config UI |

**d=1 callers of `NewAgentLoop`:** production call at `pkg/gateway/gateway.go:334`; 18 test call sites in `pkg/gateway/rest_test.go`, `rest_auth_test.go`, `reload_rollback_test.go`, `credential_redaction_test.go` — all unaffected if wiring happens at `Gateway.Start()` (outside `NewAgentLoop`).

### Relevant Execution Flows

| Process / Flow | Relevance |
|----------------|-----------|
| Gateway boot (`cmd/omnipus gateway` → `gateway.Start()`) | Primary integration point. Apply/install must run BEFORE the HTTP listener binds. |
| Subprocess spawn via `pkg/tools/shell.go:237` → `ApplyToCmd()` | Already sandboxes children. Parent-sandbox change must not double-apply on children. |
| Status endpoint `/api/system/sandbox/status` | Must flip `policy_applied: true` after wiring. Existing note string must disappear. |
| `omnipus doctor` | Should read the same status and no longer warn about the gap. |

### Cluster Placement

This feature sits in the **security-runtime** cluster (pkg/sandbox + pkg/gateway boot wiring). It spans `pkg/sandbox` (no code change, only new caller) and `pkg/gateway` (boot orchestration) — but the architectural surface area is narrow: one new call site, one new policy-builder helper, one CLI flag.

---

## 4. Phase 1.7 — Available Reference Patterns

**N/A** — nothing in `docs/reference/` specifically relates to gateway-boot-ordering for sandbox enforcement. The closest references are:

- Wave 2 security spec (`docs/plan/wave2-security-layer-spec.md`) — defines *what* Apply/Install do, not *where* to call them.
- Credential boot contract (`docs/architecture/ADR-004-credential-boot-contract.md`) — ordering-of-operations pattern for boot-time security. This spec follows the same "fail closed on error" discipline.

No external patterns reused. Implementation follows existing in-tree conventions.

---

## 5. Phase 2 — User Stories & Acceptance Criteria

### US-1 — Kernel sandbox applied to gateway at boot (Priority: P0)

As an **operator deploying Omnipus on a Linux 5.13+ server**, I want the kernel-level sandbox applied to the main gateway process at boot, so that the runtime actually matches the README's security claims and a compromised gateway cannot read `/etc/passwd` or write outside `$OMNIPUS_HOME`.

**Why this priority**: The current code ships a security feature in label only. SEC-01/02/03 are P0 requirements in the BRD. Shipping without wiring is a user-visible misrepresentation of the security posture.

**Independent Test**: Boot the gateway on Linux 6.x, `curl` the status endpoint, assert `sandbox.policy_applied == true`; from within the gateway process, attempt to write `/etc/passwd` and observe `EACCES`.

**Acceptance Scenarios**:

1. **Given** a Linux 6.x host with `gateway.sandbox.enabled = true`, **When** the gateway starts, **Then** `LinuxBackend.Apply()` and `SeccompProgram.Install()` are invoked exactly once before the HTTP listener binds, and a structured log line `sandbox.applied backend=linux landlock_abi=3 seccomp_syscalls=N` is emitted.
2. **Given** a running gateway with the sandbox applied, **When** a request for `/api/system/sandbox/status` arrives, **Then** the response has `policy_applied: true`, `seccomp_enabled: true`, and the `notes` array does NOT include the "Apply() has not been called" string.
3. **Given** a running gateway with the sandbox applied, **When** any code path in the gateway process attempts to `open("/etc/passwd", O_WRONLY)`, **Then** the syscall returns `EACCES` (Landlock denial) and no write occurs.
4. **Given** a Linux 5.10 host (pre-Landlock), **When** the gateway starts with `gateway.sandbox.enabled = true`, **Then** `SelectBackend()` returns `FallbackBackend`, Apply is a no-op, and a `sandbox.degraded reason=kernel_too_old` log line is emitted at WARN level; the gateway continues to serve requests.
5. **Given** a Linux 6.x host where the Landlock `Apply()` call returns an unexpected error, **When** `gateway.sandbox.enabled = true`, **Then** the gateway logs `sandbox.apply_failed` at ERROR, aborts boot with a non-zero exit code, and does NOT bind the HTTP listener.

### US-2 — Dev override to disable sandbox (Priority: P1)

As a **developer running the gateway locally**, I want to override sandboxing via `--sandbox=off` (CLI) or `gateway.sandbox.enabled = false` (config), so I can attach `delve`, `strace`, or `perf` without fighting kernel enforcement, and so I can bisect whether a bug is sandbox-related.

**Why this priority**: Without this override, developers cannot attach a debugger (ptrace is blocked by seccomp; filesystem probes are blocked by Landlock). That friction would cause developers to patch-and-revert the sandbox, which is worse than having an explicit knob.

**Independent Test**: Boot gateway with `--sandbox=off`; `curl` status; assert `policy_applied == false` and `disabled_by: "cli_flag"` in notes. Attach `delve attach <pid>` and confirm it works.

**Acceptance Scenarios**:

1. **Given** a Linux 6.x host, **When** the gateway starts with `--sandbox=off`, **Then** Apply/Install are not called, the status endpoint shows `policy_applied: false` with `disabled_by: cli_flag`, and a WARN log `sandbox.disabled reason=cli_flag` is emitted.
2. **Given** a Linux 6.x host with `gateway.sandbox.enabled = false` in config, **When** the gateway starts with no CLI flag, **Then** the same "disabled" path is taken and `disabled_by: config`.
3. **Given** both `--sandbox=off` CLI and `enabled = true` config, **When** the gateway starts, **Then** the CLI flag wins (CLI > config precedence) and sandbox is disabled.
4. **Given** `enabled = false` AND the host is in production (ENV `OMNIPUS_ENV=production`), **When** the gateway starts, **Then** the WARN log includes `WARN: sandbox disabled in production environment` on stderr so operators notice the misconfiguration in journald/Docker logs.

---

## 6. Phase 2.5 — Behavioral Contract, Non-Behaviors, Integration Boundaries

### Primary flow (When → Then contract)

- **When** the gateway boots on a capable Linux kernel AND sandbox is enabled, **Then** `SelectBackend()` → `computeWorkspacePolicy()` → `backend.Apply(policy)` → `seccompProgram.Install()` → status flips to `applied=true` → HTTP listener binds.
- **When** boot completes, **Then** the gateway process can read `$OMNIPUS_HOME/**`, `/proc/self/**`, `/sys/devices/system/cpu/**`, `/etc/ssl/**`, `/lib{,64}/**`, `/usr/lib{,64}/**`; can write ONLY `$OMNIPUS_HOME/**` and `/tmp/**`; cannot read `/proc/<other-pid>/**`, cannot write `/etc`, `/var`, `/root`, `/home/*/.ssh`, `/sys/firmware`.
- **When** a child process is spawned via `exec.Cmd + ApplyToCmd`, **Then** the child inherits the parent's Landlock restrictions (Landlock is inherited across fork/exec by kernel design) AND the explicit child policy from `ApplyToCmd` (both apply — the tighter of the two wins).

### Error flows

- **When** `Apply()` returns an error on a kernel that reports Landlock availability, **Then** the gateway treats it as fatal: ERROR log, exit code 78 (configuration error), no HTTP listener.
- **When** `Install()` returns an error after `Apply()` succeeded, **Then** the gateway treats it as fatal: ERROR log, exit code 78, no HTTP listener. (The gateway is already partially locked down by Landlock; continuing would leave a half-sandboxed process, which is worse than failing.)
- **When** the config file declares a path in `AllowedPaths` that doesn't exist, **Then** Apply logs a WARN for that rule (path skipped) but continues if at least one rule succeeds. Consistent with existing `LinuxBackend.Apply` ruleErrors handling.
- **When** all rule-adds fail, **Then** Apply returns an aggregate error → fatal boot abort (per above).

### Boundary conditions

- **When** `Apply()` is called a second time in the same process, **Then** the second call is a safe no-op (returns nil, does not re-invoke syscalls, does not error). Enforced via `LinuxBackend.policyApplied` flag guard.
- **When** boot occurs inside an unprivileged container without `CAP_SYS_ADMIN`, **Then** Landlock still works (it's designed for unprivileged use) but seccomp also works without caps; no behavioral change from the non-container case.
- **When** boot occurs on kernel exactly 5.13.0, **Then** Landlock ABI v1 is negotiated, only the v1 access-rights subset is applied, and a log note `landlock_abi=1 reduced_feature_set=true` is emitted.

### Explicit Non-Behaviors

- The system MUST NOT silently succeed with the sandbox effectively disabled on Linux ≥ 5.13 when the operator asked for it (`enabled = true`) — fail closed.
- The system MUST NOT crash on pre-5.13 kernels — fall back gracefully (Hard Constraint #4).
- The system MUST NOT re-apply `Apply()` within the same process — the second call is a no-op. (Landlock rulesets can be stacked, but that's a deliberate tightening. In boot flow it would be a bug.)
- The system MUST NOT emit the "Apply() has not been called" note in `/api/system/sandbox/status` after successful wiring.
- The system MUST NOT write the sandbox policy rules to disk as a side effect of boot — policy is computed in memory from config.
- The system MUST NOT require `root` to apply the sandbox (Landlock + seccomp both work unprivileged).
- The system MUST NOT apply the sandbox BEFORE credential unlock (ADR-004) — credential file reads (`$OMNIPUS_HOME/credentials.json`) need filesystem access; the sandbox allows `$OMNIPUS_HOME/**` so the ordering is "unlock → apply → install → serve". Unlock still needs to work BEFORE apply because seccomp may filter syscalls used by the KDF (Argon2id). **Correction**: Apply/Install must happen AFTER credential unlock and AFTER config load, but BEFORE HTTP listener.

### Integration Boundaries

#### Linux Kernel (Landlock LSM)
- **Data in**: `SandboxPolicy{FilesystemRules, InheritToChildren}`
- **Data out**: errno on failure; no return value on success.
- **Contract**: `landlock_create_ruleset` → `landlock_add_rule × N` → `prctl(PR_SET_NO_NEW_PRIVS)` → `landlock_restrict_self`. Per-thread; TSYNC via seccomp later.
- **On failure**: Aggregate errors, return from `Apply()`. Gateway treats as fatal.
- **Development**: Real kernel (Linux 6.8 in dev env); FallbackBackend in unit tests.

#### Linux Kernel (Seccomp-BPF)
- **Data in**: BPF program array built by `BuildSeccompProgram()`.
- **Data out**: kernel returns errno on failure.
- **Contract**: `prctl(PR_SET_NO_NEW_PRIVS)` (already set by Landlock, idempotent) → `seccomp(SECCOMP_SET_MODE_FILTER, TSYNC, prog)`.
- **On failure**: Fatal (same as Apply).
- **Development**: Real kernel.

#### Config System (Wave 1)
- **Data in**: `config.Sandbox.Enabled`, `config.Sandbox.AllowedPaths`, new `config.Sandbox.Mode` (enum: `enforce|permissive|off`).
- **Data out**: N/A.
- **Contract**: JSON under `sandbox` key. Missing = default (`enforce` on Linux ≥ 5.13, `off` elsewhere).

#### CLI (`cmd/omnipus gateway`)
- **Data in**: `--sandbox=off|enforce|permissive` flag (new).
- **Data out**: N/A.
- **Contract**: CLI > config > default.

---

## 7. Phase 3 — BDD Scenarios

### Feature: Sandbox Apply at Gateway Boot

#### Scenario: Fresh boot applies Landlock and seccomp on Linux ≥ 5.13

**Traces to**: US-1, Acceptance Scenarios 1 & 2
**Category**: Happy Path

- **Given** a Linux host reporting kernel 6.8 with Landlock ABI 3 available
- **And** `$OMNIPUS_HOME/config.json` has `gateway.sandbox.enabled = true`
- **And** no `--sandbox` CLI flag is passed
- **When** the operator runs `./omnipus gateway`
- **Then** the gateway emits a structured log entry `event=sandbox.applied backend=linux landlock_abi=3 seccomp_syscalls=N`
- **And** the HTTP listener binds after the log entry is flushed
- **And** `GET /api/system/sandbox/status` returns `{"policy_applied": true, "seccomp_enabled": true, "notes": []}`
- **But** the response must NOT contain the string "Apply() has not been called"

---

#### Scenario: Seccomp Install runs after Landlock Apply, not before

**Traces to**: US-1, Acceptance Scenario 1
**Category**: Happy Path (ordering assertion)

- **Given** a Linux 6.x host with sandbox enabled
- **When** the gateway boots and traces are captured via strace-equivalent logging
- **Then** the `landlock_restrict_self` syscall appears in the trace BEFORE the `seccomp(SECCOMP_SET_MODE_FILTER, ...)` syscall
- **And** both appear BEFORE any `bind()` or `listen()` syscall on the HTTP socket
- **But** if the order is reversed (Install first, Apply second), the test fails — because seccomp can block the `landlock_*` syscalls needed to configure the policy

> **Ordering rationale**: Seccomp is more-aggressive than Landlock (it filters ALL syscalls including `landlock_*`). If Install runs first, Apply's Landlock syscalls may be blocked. Apply must run first.

---

#### Scenario: Pre-Landlock kernel falls back gracefully

**Traces to**: US-1, Acceptance Scenario 4
**Category**: Alternate Path

- **Given** a Linux 5.10 host where Landlock is not compiled into the kernel
- **And** `gateway.sandbox.enabled = true` in config
- **When** the gateway boots
- **Then** `SelectBackend()` returns `FallbackBackend`
- **And** the gateway emits `event=sandbox.degraded reason=kernel_too_old selected_backend=fallback level=WARN`
- **And** the HTTP listener binds normally
- **And** `GET /api/system/sandbox/status` returns `{"policy_applied": false, "backend_name": "fallback", "notes": ["kernel does not support Landlock; falling back to application-level enforcement"]}`

---

#### Scenario: Non-Linux build selects fallback without attempting syscalls

**Traces to**: US-1, Acceptance Scenario 4
**Category**: Alternate Path

- **Given** a Darwin or Windows build of the gateway binary
- **And** any sandbox config
- **When** the gateway boots
- **Then** `SelectBackend()` returns `FallbackBackend`
- **And** no Linux-specific syscalls are attempted (verified by absence of `unix.Syscall` calls on the platform)
- **And** `GET /api/system/sandbox/status` returns `{"policy_applied": false, "backend_name": "fallback"}`
- **But** the gateway must boot successfully — this is a hard-constraint graceful degradation path

---

#### Scenario: Workspace policy permits $OMNIPUS_HOME writes, denies /etc

**Traces to**: US-1, Acceptance Scenario 3
**Category**: Happy Path (post-apply write probe)

- **Given** a booted gateway with sandbox applied on Linux 6.x
- **When** the gateway process attempts `os.WriteFile("$OMNIPUS_HOME/scratch/probe.txt", "ok", 0o600)`
- **Then** the write succeeds
- **And** when it attempts `os.WriteFile("/etc/passwd", "...", 0o600)`, the call returns `syscall.EACCES`
- **And** when it attempts `os.ReadFile("/proc/1/environ")`, the call returns `syscall.EACCES` (pid 1 is another process)
- **And** when it attempts `os.ReadFile("/sys/firmware/dmi/tables/smbios_entry_point")`, the call returns `syscall.EACCES`

---

#### Scenario: Dev override via CLI flag disables sandbox

**Traces to**: US-2, Acceptance Scenario 1
**Category**: Alternate Path

- **Given** a Linux 6.x host with `gateway.sandbox.enabled = true` in config
- **When** the operator runs `./omnipus gateway --sandbox=off`
- **Then** neither `Apply()` nor `Install()` is invoked
- **And** a WARN log `event=sandbox.disabled reason=cli_flag` is emitted
- **And** `GET /api/system/sandbox/status` returns `{"policy_applied": false, "disabled_by": "cli_flag"}`
- **And** a subsequent attempt to attach `delve attach <pid>` succeeds (ptrace not blocked)

---

#### Scenario: Repeated Apply() within the same process is a safe no-op

**Traces to**: US-1, Acceptance Scenario 1 (boundary)
**Category**: Edge Case

- **Given** a gateway process where `Apply()` has already returned nil
- **When** some code path (e.g., misconfigured hot-reload handler) invokes `Apply()` a second time
- **Then** the second call returns nil immediately without invoking `landlock_create_ruleset` again
- **And** an INFO log `event=sandbox.apply.skipped reason=already_applied` is emitted
- **And** the effective policy is unchanged (no tightening, no widening)

---

#### Scenario: Apply() kernel error fails the boot closed

**Traces to**: US-1, Acceptance Scenario 5
**Category**: Error Path

- **Given** a Linux 6.x host where `gateway.sandbox.enabled = true`
- **And** a (simulated) kernel condition where `landlock_create_ruleset` returns `EINVAL`
- **When** the gateway boots
- **Then** `Apply()` returns a wrapped error
- **And** the gateway emits `event=sandbox.apply_failed error="landlock: create_ruleset failed: EINVAL" level=ERROR`
- **And** the process exits with code 78 (`EX_CONFIG`)
- **And** the HTTP listener is NEVER bound — no port is opened
- **But** the gateway must NOT silently fall through to the fallback backend when the operator explicitly asked for enforcement

---

#### Scenario: Production environment with sandbox disabled logs prominent warning

**Traces to**: US-2, Acceptance Scenario 4
**Category**: Error Path (misconfiguration)

- **Given** a host where `OMNIPUS_ENV=production` is set in the environment
- **And** `gateway.sandbox.enabled = false` in config
- **When** the gateway boots
- **Then** a multi-line WARN banner is emitted to stderr containing the strings "SANDBOX DISABLED", "PRODUCTION", and "this is not the deny-by-default posture"
- **And** the banner is repeated once every 60 seconds while the gateway runs, tagged `event=sandbox.disabled.nag`

---

## 8. Phase 4 — TDD Plan

### Test Hierarchy

| Level | Scope | Purpose |
|-------|-------|---------|
| Unit | `computeWorkspacePolicy`, CLI flag parsing, mode precedence | Isolated logic, no syscalls |
| Integration | `sandbox.Apply` called via a boot-harness against a real kernel | Verify syscalls fire in the right order |
| E2E | Full gateway boot + status endpoint + filesystem probe | End-to-end operator experience |

### Test Implementation Order

| Order | Test Name | Level | Traces to BDD Scenario | Description |
|-------|-----------|-------|------------------------|-------------|
| 1 | `TestComputeWorkspacePolicy_DefaultRules` | Unit | Workspace policy permits $OMNIPUS_HOME | Asserts the generated policy contains expected read/write/exec rights on canonical paths. |
| 2 | `TestComputeWorkspacePolicy_AppendsAllowedPaths` | Unit | Workspace policy permits $OMNIPUS_HOME | Given `AllowedPaths=["/opt/shared"]`, assert it appears in the rule set with Read access. |
| 3 | `TestSandboxMode_CLIBeatsConfig` | Unit | Dev override via CLI flag | `--sandbox=off` with `enabled=true` config resolves to mode=off. |
| 4 | `TestSandboxMode_ConfigDefault` | Unit | Dev override via CLI flag | No CLI flag + `enabled=false` config → mode=off with `disabled_by=config`. |
| 5 | `TestSandboxMode_ProductionNagBanner` | Unit | Production with sandbox disabled warns | `OMNIPUS_ENV=production` + mode=off emits the multi-line banner on stderr. |
| 6 | `TestApplyIsIdempotent` | Unit (Linux-only, build tag) | Repeated Apply() is no-op | Two consecutive `backend.Apply(policy)` calls return nil; `PolicyApplied()` remains true. |
| 7 | `TestFallbackApply_IsNoOp` | Unit | Non-Linux build selects fallback | `FallbackBackend.Apply()` returns nil without side effects. |
| 8 | `TestSeccompInstallOrderAfterLandlock` | Integration (Linux 5.13+) | Seccomp Install runs after Landlock Apply | Boot harness spawns subprocess that calls Apply then Install; verify via eBPF trace the syscall order. |
| 9 | `TestGatewayBoot_AppliesSandboxOnCapableKernel` | Integration | Fresh boot applies Landlock and seccomp | Spawn gateway as subprocess, poll `/api/system/sandbox/status`, assert `policy_applied=true`. |
| 10 | `TestGatewayBoot_FailsClosedOnApplyError` | Integration | Apply() kernel error fails boot closed | Inject mock backend that returns error from Apply; assert gateway exits 78 and never binds port. |
| 11 | `TestGatewayBoot_DegradesOnPre5_13Kernel` | Integration (skipped unless `SANDBOX_TEST_KERNEL=5.10`) | Pre-Landlock kernel falls back | Simulated via build tag / env flag; asserts fallback backend selected. |
| 12 | `TestGatewayBoot_CLIFlagDisablesSandbox` | Integration | Dev override via CLI flag | Spawn gateway with `--sandbox=off`; assert `policy_applied=false` and `disabled_by=cli_flag`. |
| 13 | `TestGatewayBoot_WorkspaceWriteAllowed` | E2E (Linux) | Workspace policy permits $OMNIPUS_HOME | Post-boot, invoke a test-only tool that writes `$OMNIPUS_HOME/scratch/probe.txt`; assert success. |
| 14 | `TestGatewayBoot_EtcPasswdDenied` | E2E (Linux) | Workspace policy denies /etc/passwd | Post-boot, invoke test tool that attempts write to `/etc/passwd`; assert EACCES returned via tool output. |
| 15 | `TestGatewayBoot_ProcOtherPidDenied` | E2E (Linux) | Workspace policy denies /proc | Post-boot, read `/proc/1/environ`; assert EACCES. |
| 16 | `TestSandboxStatus_NoteRemovedAfterWiring` | E2E | Fresh boot applies Landlock and seccomp (AS-2) | Post-boot, `GET /api/system/sandbox/status`, assert `notes` does not contain "Apply() has not been called". |

### Test Datasets

#### Dataset A: SandboxPolicy Rule Computation

| # | Input (config) | Boundary Type | Expected Output | Traces to | Notes |
|---|---------------|---------------|-----------------|-----------|-------|
| 1 | `AllowedPaths=[]` | Empty | Policy contains default rules only (home, /tmp, /proc/self, /lib, /usr, /etc/ssl) | Workspace policy permits $OMNIPUS_HOME | Baseline |
| 2 | `AllowedPaths=["/opt/shared"]` | Single item | Default rules + `{Path:"/opt/shared", Access:Read}` | Workspace policy permits $OMNIPUS_HOME | User extension |
| 3 | `AllowedPaths=["/etc"]` | Overlap with system | Policy rule added but log WARN `overlap_with_system=true` | Workspace policy permits $OMNIPUS_HOME | Defensive |
| 4 | `AllowedPaths=["/nonexistent/path"]` | Missing file | Rule add fails inside `Apply()`, aggregated into ruleErrors; boot continues if other rules succeed | Apply() kernel error (partial) | Consistent with existing ruleErrors semantics |
| 5 | `AllowedPaths=[""]` | Empty string | Rejected at config-validation layer (not reached) | N/A | Config validation |
| 6 | `AllowedPaths=["../../../etc"]` | Traversal | Cleaned via `filepath.Clean`; resolves to `/etc`; overlap warning emitted | Workspace policy permits $OMNIPUS_HOME | Path sanitisation |

#### Dataset B: Kernel & OS Axis × Fresh-Install vs Existing-Creds

| # | OS | Kernel | Install State | Sandbox Config | Expected Mode | Expected Boot | Traces to |
|---|-----|--------|---------------|----------------|--------------|---------------|-----------|
| 1 | Linux | 6.8 | fresh | `enabled=true` | enforce | `policy_applied=true` | Fresh boot applies Landlock (HP) |
| 2 | Linux | 6.8 | existing credentials.json | `enabled=true` | enforce | `policy_applied=true`; unlock succeeds (sandbox allows $OMNIPUS_HOME) | Workspace policy permits $OMNIPUS_HOME |
| 3 | Linux | 5.13.0 | fresh | `enabled=true` | enforce (ABI v1) | `policy_applied=true, landlock_abi=1` | Fresh boot applies Landlock (HP) |
| 4 | Linux | 5.10 | fresh | `enabled=true` | fallback | `policy_applied=false, degraded` | Pre-Landlock fallback |
| 5 | Linux | 6.8 | fresh | `enabled=false` | off | `policy_applied=false, disabled_by=config` | Dev override via CLI flag (AS-2) |
| 6 | Linux | 6.8 | fresh | `enabled=true` + `--sandbox=off` | off | `policy_applied=false, disabled_by=cli_flag` | Dev override via CLI flag (AS-1) |
| 7 | Darwin | 23.x | fresh | `enabled=true` | fallback | `policy_applied=false` | Non-Linux build selects fallback |
| 8 | Windows | 10.0.22621 | fresh | `enabled=true` | fallback | `policy_applied=false` | Non-Linux build selects fallback |
| 9 | Linux (Termux) | 5.15 Android | fresh | `enabled=true` | fallback | `policy_applied=false, reason=termux_no_landlock` | Pre-Landlock fallback (Android) |
| 10 | Linux | 6.8 | fresh | `enabled=true`, kernel returns EINVAL on create_ruleset | enforce→fatal | Boot exits 78, no listener | Apply() kernel error (EP) |

### Regression Test Requirements

> New capability — no existing Apply-at-boot behaviour to preserve. Integration seams protected by:

| Existing Behaviour | Existing Test | Regression Risk |
|--------------------|---------------|-----------------|
| `ApplyToCmd()` for child processes works | `pkg/tools/shell_test.go`, `tests/security/sandbox_enforcement_linux_test.go` | MUST still pass — new code must not break child sandbox inheritance. |
| Gateway boots without sandbox wiring (current behaviour, to be deprecated) | All `pkg/gateway/rest_*_test.go` | These tests use `FallbackBackend` implicitly via `NewAgentLoop`; they must still pass because they operate below the `Gateway.Start()` layer. |
| `omnipus doctor` reports sandbox status | `pkg/cli/doctor_test.go` (if present) | After wiring, the doctor output changes — update snapshot/golden if applicable. |

---

## 9. Phase 5 — Functional Requirements & Success Criteria

### Functional Requirements

- **FR-J-001** — The gateway MUST invoke `sandboxBackend.Apply(policy)` exactly once during boot on Linux ≥ 5.13 when `gateway.sandbox.enabled = true`, before `ListenAndServe` is called.
- **FR-J-002** — The gateway MUST invoke `seccompProgram.Install()` exactly once, strictly AFTER `Apply()` returns successfully, and before `ListenAndServe`.
- **FR-J-003** — The gateway MUST compute the sandbox policy from `$OMNIPUS_HOME`, system-library paths (`/lib`, `/lib64`, `/usr`, `/etc/ssl`), temp (`/tmp`), self-proc (`/proc/self`), and the `Sandbox.AllowedPaths` config list.
- **FR-J-004** — The gateway MUST fail closed (exit non-zero, no HTTP listener) when `Apply()` or `Install()` returns an error on a kernel that claims support.
- **FR-J-005** — The gateway MUST select `FallbackBackend` on kernels below 5.13 and on non-Linux builds, and MUST continue to boot successfully without Apply/Install.
- **FR-J-006** — The gateway MUST accept a `--sandbox=off|enforce|permissive` CLI flag that takes precedence over `gateway.sandbox.enabled` in the config.
- **FR-J-007** — The gateway SHOULD emit structured log events `sandbox.applied`, `sandbox.degraded`, `sandbox.disabled`, `sandbox.apply_failed`, `sandbox.apply.skipped` with consistent field sets (`backend`, `landlock_abi`, `seccomp_syscalls`, `reason`, `disabled_by`).
- **FR-J-008** — `GET /api/system/sandbox/status` MUST return `policy_applied=true, seccomp_enabled=true, notes=[]` (no "Apply() has not been called" note) after successful apply.
- **FR-J-009** — `LinuxBackend.Apply()` MUST be idempotent within a single process — a second invocation returns nil without invoking kernel syscalls.
- **FR-J-010** — The gateway MUST NOT invoke Apply before credential unlock or config load, and MUST invoke Apply before binding the HTTP listener (ordering: unlock → load config → select backend → Apply → Install → bind).
- **FR-J-011** — When `OMNIPUS_ENV=production` AND mode=off, the gateway MUST emit a multi-line WARN banner on stderr at boot and every 60 seconds.

### Success Criteria

- **SC-J-001** — On a Linux 6.x host after boot, attempting to write `/etc/passwd` from within the gateway process returns `EACCES` within 1ms (kernel-enforced, no tail).
- **SC-J-002** — On a Linux 6.x host after boot, `curl localhost:3000/api/system/sandbox/status` returns JSON with `policy_applied: true` within 50ms of the first successful `/health` request.
- **SC-J-003** — On a Linux 5.10 host, the gateway completes boot (binds HTTP listener) within 2 seconds and `policy_applied: false` with `backend_name: "fallback"`.
- **SC-J-004** — The sandbox wiring adds no more than 5ms to cold boot time on Linux 6.x (measured as the delta between `SelectBackend()` return and `Install()` return).
- **SC-J-005** — The total RAM overhead added by Apply + Install is ≤ 512KB (BPF program + ruleset fd are tiny; the CLAUDE.md 10MB budget has ample headroom).
- **SC-J-006** — The integration test suite (`go test ./tests/security/...`) passes on Linux with the new wiring enabled; no flake rate > 1% over 100 runs.
- **SC-J-007** — `/api/system/sandbox/status` never contains the string "Apply() has not been called" after a successful wired boot (test: `grep -c` on response body == 0).
- **SC-J-008** — When `--sandbox=off`, `ptrace(PTRACE_ATTACH, <gateway_pid>, ...)` from a test harness succeeds within 100ms (dev debugging unblocked).

### Traceability Matrix

| FR | User Story | BDD Scenario(s) | Test Name(s) |
|----|-----------|-----------------|--------------|
| FR-J-001 | US-1 | Fresh boot applies Landlock and seccomp; Seccomp Install runs after Landlock Apply | TestGatewayBoot_AppliesSandboxOnCapableKernel; TestSeccompInstallOrderAfterLandlock |
| FR-J-002 | US-1 | Seccomp Install runs after Landlock Apply | TestSeccompInstallOrderAfterLandlock |
| FR-J-003 | US-1 | Workspace policy permits $OMNIPUS_HOME writes, denies /etc | TestComputeWorkspacePolicy_DefaultRules; TestComputeWorkspacePolicy_AppendsAllowedPaths; TestGatewayBoot_WorkspaceWriteAllowed; TestGatewayBoot_EtcPasswdDenied |
| FR-J-004 | US-1 | Apply() kernel error fails the boot closed | TestGatewayBoot_FailsClosedOnApplyError |
| FR-J-005 | US-1 | Pre-Landlock kernel falls back gracefully; Non-Linux build selects fallback | TestGatewayBoot_DegradesOnPre5_13Kernel; TestFallbackApply_IsNoOp |
| FR-J-006 | US-2 | Dev override via CLI flag disables sandbox | TestSandboxMode_CLIBeatsConfig; TestSandboxMode_ConfigDefault; TestGatewayBoot_CLIFlagDisablesSandbox |
| FR-J-007 | US-1, US-2 | All scenarios assert on log strings | All integration tests assert log output |
| FR-J-008 | US-1 | Fresh boot applies Landlock and seccomp (AS-2) | TestSandboxStatus_NoteRemovedAfterWiring |
| FR-J-009 | US-1 | Repeated Apply() within the same process is a safe no-op | TestApplyIsIdempotent |
| FR-J-010 | US-1 | Fresh boot applies Landlock and seccomp (ordering) | TestSeccompInstallOrderAfterLandlock (extended to assert listener-bind happens last) |
| FR-J-011 | US-2 | Production environment with sandbox disabled logs prominent warning | TestSandboxMode_ProductionNagBanner |

**Completeness check**: Every FR-J-* row has ≥1 BDD scenario and ≥1 test. Every BDD scenario appears in ≥1 row. ✓

---

## 10. Phase 5.5 — Ambiguity Self-Audit

| # | What's Ambiguous | Assumed Behaviour (flag for user decision) | Question to Resolve |
|---|------------------|--------------------------------------------|---------------------|
| 1 | Behaviour of `/health` in the ~10ms window between `Apply()` returning and `Install()` returning | Assumed: `/health` listener not yet bound during this window, so /health cannot be called — Apply/Install happen BEFORE `net.Listen`. | Confirm listener is bound last. If there's a pre-HTTP readiness probe (systemd-notify, Kubernetes startup), where does it fit? |
| 2 | Should `permissive` mode (log-only, no enforce) exist in Sprint J, or is it deferred? | Assumed: deferred. Mode enum accepts `permissive` but treats it same as `off` for now, with a TODO. | Does the wave-2 security spec require a permissive mode? If yes, add FR-J-012 and two more scenarios. |
| 3 | When `Sandbox.AllowedPaths` contains a path that overlaps with a restricted system path (e.g., `/etc`), does the user rule win or does the default-deny win? | Assumed: user rule wins for read-only; write access to `/etc` still denied. Logged at WARN. | User may want strict "never allow /etc". Is there an explicit deny-list? |
| 4 | What exit code should the gateway use when Apply/Install fails? | Assumed: 78 (`EX_CONFIG`). | Is there an existing exit-code convention? Check `cmd/omnipus/main.go`. |
| 5 | Should the production nag banner be suppressed if a reason for disabling is documented in config (e.g., `sandbox.disabled_reason = "CI runner"` field)? | Assumed: no suppression mechanism in Sprint J. Banner fires unconditionally when `OMNIPUS_ENV=production` + mode=off. | Do we need a suppress flag? |
| 6 | Do we need to handle `SIGHUP`-style config reload that would attempt a second `Apply()`? | Assumed: reload does not re-invoke Apply (idempotent guard handles it harmlessly). Reload to enable/disable sandbox is NOT supported — requires restart. | Confirm hot-reload scope for sandbox config. |
| 7 | Should seccomp be installed even when Landlock falls back to fallback backend? | Assumed: no. Seccomp is gated on the same LinuxBackend path. If LinuxBackend is not selected, Install is not called. Consistency with existing test-only Install callers. | Is seccomp-alone (without Landlock) a valid intermediate state on, e.g., older kernels with seccomp but no Landlock? |

---

## 11. Phase 5.7 — Holdout Evaluation Scenarios

> **Not referenced in TDD plan or traceability matrix.** For post-implementation verification only.

### Happy Path

- **H1 — Cold start under load**
  - **Setup**: Pre-warm 100 HTTP clients, then start gateway.
  - **Action**: Measure time-to-first-200 on `/health`.
  - **Expected**: Apply + Install + bind completes within 100ms; no 503s.

- **H2 — Sandbox applied, agent runs a tool that writes to workspace**
  - **Setup**: Boot with sandbox enabled; send chat message asking the agent to run `write_file $OMNIPUS_HOME/sessions/probe.txt`.
  - **Action**: Observe tool call result.
  - **Expected**: Tool succeeds (workspace is writable).

- **H3 — Sandbox applied, agent rejected when writing outside workspace**
  - **Setup**: Same as H2.
  - **Action**: Agent attempts `write_file /tmp/../etc/malicious`.
  - **Expected**: Tool returns EACCES; audit log records the denial.

### Error

- **E1 — Disk full during boot**
  - **Setup**: Mount `$OMNIPUS_HOME` on a filesystem with 0 bytes free.
  - **Action**: Boot gateway.
  - **Expected**: Boot fails BEFORE Apply (config write fails first); clean error message. Apply/Install should not have been attempted.

- **E2 — Sandbox flag typo**
  - **Setup**: Pass `--sandbox=of` (typo).
  - **Action**: Boot gateway.
  - **Expected**: Clear error `invalid sandbox mode "of"; must be one of: enforce, permissive, off`; exit code 2 (usage error); no boot.

### Edge

- **G1 — Very long `AllowedPaths` list (500 entries)**
  - **Setup**: Config with 500 valid paths.
  - **Action**: Boot gateway.
  - **Expected**: Apply succeeds; all 500 rules added; boot time within SC-J-004 budget.

- **G2 — `AllowedPaths` contains `$OMNIPUS_HOME` explicitly (duplicate)**
  - **Setup**: Config lists `$OMNIPUS_HOME` that's already in default rules.
  - **Action**: Boot gateway.
  - **Expected**: Duplicate deduplicated (or two rules with same path, kernel handles it); no error; boot succeeds.

---

## Assumptions

- The `SandboxBackend` interface at `pkg/sandbox/sandbox.go:40-46` is stable for Sprint J — no shape changes.
- `LinuxBackend.Apply()` already tracks its own `policyApplied` state correctly (`pkg/sandbox/sandbox_linux.go:121`) and returns nil on repeat calls; if it doesn't, the FR-J-009 guard must be added inside the backend, not in the caller.
- The gateway's current boot order (credential unlock → config load → `NewAgentLoop` → services → listen) is correct up to the `NewAgentLoop` return; the new Apply/Install call slots in between `NewAgentLoop` and `http.ListenAndServe`.
- `OMNIPUS_ENV=production` is the agreed environment-identifier convention (flag for user if not).
- Non-Linux builds already route through `selectBackendPlatform` to return `FallbackBackend` — verified for Linux and needs confirmation for Darwin/Windows build tags in Phase 5.5 ambiguity #2.

## Clarifications

### 2026-04-20

- **Q**: Should the sprint name be `sprint-j-sandbox-apply` (narrow) or `sprint-j-security-hardening` (broad)? **A**: Branch is already `sprint-j-security-hardening`; spec filename is `sprint-j-sandbox-apply-spec.md` to scope tightly to issue #76. Future hardening work can add sibling specs on the same branch.
- **Q**: Does `Apply()` need a `context.Context` parameter for cancellation? **A**: No — Apply is a one-shot prctl+syscall sequence measured in microseconds. Adding ctx would be API churn for zero benefit.
