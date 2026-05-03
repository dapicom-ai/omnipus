// Package sandbox — hardened-exec child wrapper for Tier 2 (build_static)
// and Tier 3 (web_serve dev mode + workspace.shell_bg) tools.
//
// Cross-platform contract:
//
// - Linux: Setpgid + Pdeathsig=SIGTERM for clean parent-death cleanup,
// plus prlimit on RLIMIT_AS (memory) and RLIMIT_CPU (cpu seconds).
// Children inherit gateway Landlock + seccomp unchanged (,
// no narrowing in v4).
// - macOS: prlimit-equivalent via Setrlimit (RLIMIT_AS unsupported on
// darwin; uses RLIMIT_DATA which approximates heap+stack). No isolation
// primitive beyond OS perms.
// - Windows: Job Object with JOBOBJECT_EXTENDED_LIMIT_INFORMATION
// (process-memory limit + KILL_ON_JOB_CLOSE so the child dies if the
// gateway exits). No DACL/Restricted Token/AppContainer in v4.
//
// All platforms inject HTTP_PROXY/HTTPS_PROXY when EgressProxyAddr is non-
// empty and npm_config_cache when WorkspaceDir is non-empty ( — per-
// agent npm cache so concurrent builds don't fight over a shared cache).
//
// Threat note: Tier 2 and Tier 3 are documented as trusted-prompt features
//. The child has the gateway's full filesystem reach;
// raw TCP egress is unblocked. Operator awareness is the primary control.

package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// sensitiveEnvKeys are stripped from any inherited gateway environment before
// it reaches a child process. Includes the master key, the bearer auth token,
// and the path to the master key file so children cannot read them.
var sensitiveEnvKeys = map[string]struct{}{
	"OMNIPUS_MASTER_KEY":   {},
	"OMNIPUS_KEY_FILE":     {},
	"OMNIPUS_BEARER_TOKEN": {},
}

// scrubGatewayEnv returns a copy of os.Environ() with sensitive keys removed.
// Used by mergeEnv so children inherit PATH/HOME/LANG without seeing secrets.
func scrubGatewayEnv() []string {
	parent := os.Environ()
	out := make([]string, 0, len(parent))
	for _, kv := range parent {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		if _, blocked := sensitiveEnvKeys[kv[:eq]]; blocked {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Limits describes resource caps and environment hints for a hardened child.
//
// Zero values are interpreted as "no limit" for TimeoutSeconds and
// MemoryLimitBytes; callers SHOULD pass concrete defaults from config (e.g.
// cfg.Tools.BuildStatic.TimeoutSeconds = 300, MemoryLimitBytes = 512 MiB)
// rather than relying on unbounded execution.
type Limits struct {
	// TimeoutSeconds is the wall-clock hard-kill deadline. 0 disables the
	// timeout (the parent context governs cancellation instead). On Linux
	// this is enforced via context cancellation; we do NOT use RLIMIT_CPU
	// because cpu-time != wall-clock and a build that sleeps for I/O could
	// run forever.
	TimeoutSeconds int32

	// MemoryLimitBytes caps the child's address space (Linux RLIMIT_AS,
	// Windows JOB_OBJECT_LIMIT_PROCESS_MEMORY). 0 disables the cap. Values
	// below 64 MiB are not validated here — the BuildStatic boot validator
	// rejects them earlier in the call chain.
	MemoryLimitBytes uint64

	// WorkspaceDir is the agent's workspace root. When non-empty,
	// npm_config_cache is set to <WorkspaceDir>/.npm-cache.
	// This is also the cwd of the child process.
	WorkspaceDir string

	// EgressProxyAddr is the loopback address (e.g. "127.0.0.1:54321") of
	// the egress proxy started by the caller. When non-empty, HTTP_PROXY
	// and HTTPS_PROXY are injected into the child's environment so HTTP/
	// HTTPS traffic flows through the allow-listed proxy.
	//
	// acknowledgement: this is HTTP/HTTPS only. Raw TCP connect
	// is NOT covered. Documented as a trusted-prompt-feature limitation.
	EgressProxyAddr string
}

// Result captures the outcome of a hardened child execution.
type Result struct {
	// Stdout and Stderr are the captured byte streams. Both are bounded
	// to 4 MiB each; longer output is truncated with a notice appended.
	Stdout []byte
	Stderr []byte

	// ExitCode is the child's exit code. -1 indicates the process was
	// killed by a signal (e.g. SIGTERM on timeout); inspect TimedOut.
	ExitCode int

	// TimedOut is true when the wall-clock deadline elapsed before the
	// child exited on its own. The child receives SIGTERM, then SIGKILL
	// after a 5-second grace period.
	TimedOut bool

	// Duration is the elapsed wall-clock time of the child run.
	Duration time.Duration

	// MemoryLimitUnsupported is true when the caller requested a non-zero
	// MemoryLimitBytes but the platform-specific post-start hardener could
	// not enforce it (currently darwin only — RLIMIT_AS unsupported on XNU).
	// Linux and Windows always set this to false because both backends
	// enforce the cap or fail the spawn outright.
	//
	// Tooling SHOULD surface this to the agent via the result preamble so
	// operators of cross-platform builds know the cap is best-effort. See
	// HIGH-1 (silent-failure-hunter).
	MemoryLimitUnsupported bool
}

// outputCap bounds captured stdout/stderr per stream. 4 MiB is enough for
// build logs while preventing a runaway child from exhausting gateway memory.
const outputCap = 4 << 20

// proxyGracePeriod is how long the parent waits between SIGTERM and SIGKILL
// when a hardened child exceeds its timeout. Must be long enough for npm /
// node to flush stdout but short enough that operators don't notice.
const proxyGracePeriod = 5 * time.Second

// ErrEmptyArgv is returned by Run when called with no command to execute.
var ErrEmptyArgv = errors.New("hardened_exec: argv is empty")

// Run launches argv as a hardened child process.
//
// The child:
// - Runs in WorkspaceDir as cwd (when set).
// - Has the merged env: caller-supplied env, then proxy + npm cache vars
// appended last so callers cannot accidentally override them.
// - Is bounded by Limits — wall-clock timeout, address-space cap.
// - Has captured stdout/stderr (each capped at 4 MiB).
//
// Returns ErrEmptyArgv if argv has zero length. Other returned errors
// indicate the child could not be started; once started, normal exit codes
// are reported via Result.ExitCode regardless of zero/non-zero.
func Run(ctx context.Context, argv []string, env []string, lim Limits) (Result, error) {
	if len(argv) == 0 {
		return Result{}, ErrEmptyArgv
	}

	// Linux Landlock enforces per-OS-thread, but Go's M:N scheduler routes
	// goroutines onto arbitrary worker threads. If we forked the child from
	// whichever worker thread happened to run this goroutine, it would
	// almost certainly be a thread that never had restrict_self called on
	// it, and the child would silently escape the gateway's kernel sandbox.
	// To close that gap we run the entire spawn-and-wait sequence inside a
	// dedicated goroutine that locks itself to a fresh OS thread, re-applies
	// the Landlock domain to that thread, and exits without unlocking — so
	// Go's runtime disposes of the (now-restricted) thread instead of
	// recycling it for unrelated work. On non-enforce modes and non-Linux
	// platforms restrictCurrentThreadIfNeeded is a no-op and the wrapper
	// only adds the overhead of one extra goroutine per spawn.
	type runResult struct {
		res Result
		err error
	}
	ch := make(chan runResult, 1)
	go func() {
		runtime.LockOSThread()
		// Intentionally NO UnlockOSThread — see comment above. The
		// goroutine returns at the end of this function, which causes the
		// runtime to terminate the locked thread.
		if err := restrictCurrentThreadIfNeeded(); err != nil {
			ch <- runResult{Result{}, fmt.Errorf("hardened_exec: per-thread restrict: %w", err)}
			return
		}
		res, err := runOnCurrentThread(ctx, argv, env, lim)
		ch <- runResult{res, err}
	}()
	r := <-ch
	return r.res, r.err
}

// StartLocked invokes cmd.Start on an OS thread that has the gateway's
// Landlock domain re-applied to it, so the forked child inherits the kernel
// sandbox even when Go's M:N scheduler would otherwise have routed the
// spawning goroutine onto an unrestricted worker thread. The launching
// goroutine then exits without UnlockOSThread, which causes Go's runtime to
// terminate the (permanently restricted) OS thread instead of recycling it
// for unrelated work.
//
// After this returns, the caller can safely cmd.Wait, cmd.Process.Kill,
// cmd.Process.Signal, etc. from any goroutine — those operations do not
// require the launching thread.
//
// IMPORTANT: PR_SET_PDEATHSIG is cleared on cmd before Start, because the
// launching OS thread dies as soon as the goroutine returns. If a caller
// (e.g. ApplyChildHardening) has set Pdeathsig=SIGTERM, the kernel would
// fire that signal at the child the moment Go retires the locked thread.
// StartLocked therefore unconditionally clears Pdeathsig — children launched
// through this helper survive the launching thread's death and are managed
// by their own lifecycle owner (DevServerRegistry, session manager, etc.).
//
// On non-Linux platforms or when the sandbox is disabled,
// restrictCurrentThreadIfNeeded is a no-op and this wrapper costs only one
// goroutine + channel hop per spawn.
func StartLocked(cmd *exec.Cmd) error {
	clearPdeathsigForBackground(cmd)
	ch := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		// Intentionally NO UnlockOSThread.
		if err := restrictCurrentThreadIfNeeded(); err != nil {
			ch <- fmt.Errorf("hardened_exec: per-thread restrict: %w", err)
			return
		}
		ch <- cmd.Start()
	}()
	return <-ch
}

// runOnCurrentThread is the original Run body, kept private so Run can wrap
// it in a thread-locked goroutine without duplicating the spawn logic.
func runOnCurrentThread(ctx context.Context, argv []string, env []string, lim Limits) (Result, error) {
	// Set up the wall-clock timeout. We always derive a child context so
	// the caller's ctx can still cancel earlier than TimeoutSeconds.
	runCtx := ctx
	var cancel context.CancelFunc
	if lim.TimeoutSeconds > 0 {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(lim.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	if lim.WorkspaceDir != "" {
		cmd.Dir = lim.WorkspaceDir
	}
	cmd.Env = mergeEnv(env, lim)

	// Bounded stdout/stderr capture. We use bytes.Buffer with manual length
	// checks rather than io.LimitWriter because we want a clear truncation
	// notice in the captured output rather than a silent cut.
	var stdoutBuf, stderrBuf cappedBuffer
	stdoutBuf.cap = outputCap
	stderrBuf.cap = outputCap
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	// Stdin: bind to an empty in-memory reader so os/exec does not open
	// /dev/null on our behalf. Landlock at the gateway level does not grant
	// access to /dev, and an empty reader yields the same EOF semantics as
	// /dev/null without traversing the device tree.
	cmd.Stdin = bytes.NewReader(nil)

	// Apply platform-specific hardening (Setpgid+Pdeathsig+prlimit on
	// Linux, rlimit on darwin, Job Object on windows). Failure to apply
	// hardening returns an error before Start so the caller can decide
	// whether to fall back or abort. We do NOT silently spawn an
	// unhardened child.
	if err := applyPlatformHardening(cmd, lim); err != nil {
		return Result{}, fmt.Errorf("hardened_exec: apply hardening: %w", err)
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("hardened_exec: start %s: %w", argv[0], err)
	}

	// Apply post-start hardening (Windows Job Object assignment must run
	// after Start because we need the child's process Handle).
	if err := applyPostStartHardening(cmd, lim); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return Result{}, fmt.Errorf("hardened_exec: post-start hardening: %w", err)
	}

	waitErr := cmd.Wait()
	duration := time.Since(start)

	// Determine timeout vs natural exit. exec.CommandContext kills the
	// child when the context expires; we detect that by inspecting the
	// derived context's error, NOT the original ctx (which may still be
	// live if only the timeout fired).
	timedOut := false
	if runCtx.Err() == context.DeadlineExceeded {
		timedOut = true
	}

	exitCode := -1
	if waitErr == nil {
		exitCode = 0
	} else {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exitCode = ee.ExitCode()
		}
	}

	// HIGH-1: surface platform limitations to the caller. Set
	// MemoryLimitUnsupported when the caller asked for a non-zero cap but
	// this platform cannot enforce it (currently darwin only). Tooling
	// that wraps Run uses this flag to include a warning in the result
	// preamble shown to the agent.
	memUnsupported := lim.MemoryLimitBytes > 0 && !memoryLimitSupported

	return Result{
		Stdout:                 stdoutBuf.Bytes(),
		Stderr:                 stderrBuf.Bytes(),
		ExitCode:               exitCode,
		TimedOut:               timedOut,
		Duration:               duration,
		MemoryLimitUnsupported: memUnsupported,
	}, nil
}

// mergeEnv merges the gateway's (scrubbed) environment, the caller's env,
// and platform-injected vars. Order: gateway env, caller env, injected
// proxy/cache vars. POSIX exec(3) gives later entries precedence on
// duplicate keys, so caller env can override gateway env, and injected
// proxy/cache vars override both — this is intentional.
//
// Gateway env is always inherited (with sensitive keys stripped) so children
// have working PATH/HOME/LANG without leaking secrets. Setting cmd.Env to a
// non-nil empty slice in os/exec means "no inheritance"; the previous
// implementation forced operators to plumb PATH through the tool's `env`
// arg, which silently broke commands like `npm run dev` that depend on
// node_modules/.bin being on PATH.
func mergeEnv(env []string, lim Limits) []string {
	gateway := scrubGatewayEnv()
	merged := make([]string, 0, len(gateway)+len(env)+6)
	merged = append(merged, gateway...)
	merged = append(merged, env...)

	// Inject proxy variables. Both upper- and lower-case forms are widely
	// honoured (npm/node use lowercase, curl uses uppercase, Go's
	// http.Transport uses both via httpproxy.FromEnvironment).
	if lim.EgressProxyAddr != "" {
		proxyURL := "http://" + lim.EgressProxyAddr
		merged = append(merged,
			"HTTP_PROXY="+proxyURL,
			"HTTPS_PROXY="+proxyURL,
			"http_proxy="+proxyURL,
			"https_proxy="+proxyURL,
			// NO_PROXY for loopback so node's own loopback connections
			// (e.g. dev-server bind probes) don't bounce off the proxy.
			"NO_PROXY=127.0.0.1,localhost,::1",
			"no_proxy=127.0.0.1,localhost,::1",
		)
	}

	// Per-agent npm cache. The boot validator ensures
	// WorkspaceDir is absolute, so the joined path is well-formed.
	if lim.WorkspaceDir != "" {
		// Use forward-slash join — npm normalises path separators on
		// Windows, and using filepath.Join here would import filepath
		// just for one call. Forward slashes work everywhere npm runs.
		merged = append(merged,
			"npm_config_cache="+lim.WorkspaceDir+"/.npm-cache",
		)
	}

	return merged
}

// cappedBuffer is a bytes.Buffer wrapper that enforces a hard ceiling on
// bytes written. Once the cap is reached further writes are accepted (so
// the underlying child does not block on a full pipe) but discarded; a
// truncation notice is appended to the captured bytes on Bytes.
type cappedBuffer struct {
	buf       bytes.Buffer
	cap       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.cap - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		// Lie about the byte count — the kernel's pipe-fill semantics
		// require us to report success or the child will see EPIPE.
		return len(p), nil
	}
	if len(p) > remaining {
		c.truncated = true
		c.buf.Write(p[:remaining])
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

// Bytes returns the captured bytes, with a truncation notice appended when
// any data was discarded. The notice is human-readable so it shows up
// in error reports and stderr tails without further processing.
func (c *cappedBuffer) Bytes() []byte {
	if !c.truncated {
		return c.buf.Bytes()
	}
	// Append the notice without mutating the underlying buffer in case
	// the caller calls Bytes multiple times.
	notice := []byte("\n[hardened_exec: output truncated at " + formatBytes(c.cap) + "]\n")
	out := make([]byte, 0, c.buf.Len()+len(notice))
	out = append(out, c.buf.Bytes()...)
	out = append(out, notice...)
	return out
}

// formatBytes returns a short human-readable size string used only for the
// truncation notice. Avoids importing humanize for a single call.
func formatBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%d MiB", n>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%d KiB", n>>10)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// ApplyChildHardening exposes the platform-specific pre-start hardening
// (Setpgid, Pdeathsig on Linux; Setpgid on darwin; no-op on windows) so
// callers that need a non-blocking spawn (e.g. web_serve dev-mode and
// workspace.shell_bg) can apply the same primitives without going through
// Run.
//
// Caller responsibility:
// - Build the *exec.Cmd themselves (cmd.Dir, cmd.Env, etc.).
// - Call ApplyChildHardening BEFORE cmd.Start.
// - Call ApplyChildPostStartHardening AFTER cmd.Start.
//
// This is the same sequence Run performs internally; exposing it lets
// the dev-server tool reuse the platform logic without copy-pasting the
// SysProcAttr setup.
func ApplyChildHardening(cmd *exec.Cmd, lim Limits) error {
	return applyPlatformHardening(cmd, lim)
}

// ApplyChildPostStartHardening exposes the post-Start step (prlimit on
// Linux, Job Object assignment on Windows, no-op on darwin). See
// ApplyChildHardening for the contract.
func ApplyChildPostStartHardening(cmd *exec.Cmd, lim Limits) error {
	return applyPostStartHardening(cmd, lim)
}

// StderrTail returns the last n bytes of stderr (or all of it when shorter).
// Useful for surfacing build failures to the agent without dumping the entire
// log into the LLM context.
func (r Result) StderrTail(n int) string {
	if len(r.Stderr) <= n {
		return string(r.Stderr)
	}
	tail := r.Stderr[len(r.Stderr)-n:]
	// Cut at the first newline so the tail starts at a line boundary —
	// avoids garbled mid-line breaks when surfaced in error messages.
	if idx := bytes.IndexByte(tail, '\n'); idx >= 0 && idx < len(tail)-1 {
		tail = tail[idx+1:]
	}
	return strings.TrimSpace(string(tail))
}
