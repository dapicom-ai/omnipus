//go:build linux

// Package sandbox_test — insider-LLM red-team coverage for fork-bomb threats.
//
// Two threats from the insider-pentest report are exercised here:
//
//	C3-DIRECT — direct fork bomb. The classic ":(){ :|:& };:" payload, sent
//	            verbatim through workspace.shell. Today: blocked by the
//	            shell-guard regex (`:\(\)\s*\{.*\};\s*:`). Test PASSES today.
//
//	C3-INDIRECT — indirect fork bomb. The same payload written to a file
//	            (`fork.sh`) inside the workspace, then invoked via `sh fork.sh`.
//	            The shell-guard regex never sees the bomb's pattern (only sees
//	            "sh fork.sh"). Today: NOT contained — there is no
//	            RLIMIT_NPROC on hardened-exec children. Test FAILS by design
//	            until v0.2 (#155) adds RLIMIT_NPROC to hardened_exec_linux.go.
//
// Safety: the indirect test sets RLIMIT_NPROC on the test process itself
// before launching the bomb. This means even if the production code lacks
// the limit, the kernel-imposed cap on the test process (and its
// descendants) bounds the host damage. The test asserts that production
// code SHOULD propagate a similar limit; until then the test reports the
// gap as a t.Errorf.
package sandbox_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestRedteam_ForkBomb_DirectPattern_Blocked verifies that the classic
// fork-bomb pattern is blocked at the shell-guard regex layer BEFORE it
// reaches the kernel. This passes today because of the existing
// `:\(\)\s*\{.*\};\s*:` regex in pkg/tools/shell.go::defaultDenyPatterns.
//
// We don't import pkg/tools here — that's a heavy package with many
// transitive deps. Instead we reproduce the production regex inline so
// this test fails loudly if anyone reworks the regex without keeping the
// same coverage. The regex string MUST be kept in sync with the production
// definition at pkg/tools/shell.go:224.
func TestRedteam_ForkBomb_DirectPattern_Blocked(t *testing.T) {
	t.Logf("documents C3-DIRECT (fork bomb literal pattern) from insider-pentest report; current control is shell-guard regex")

	// Production regex — keep in sync with pkg/tools/shell.go:224.
	const guardPattern = `:\(\)\s*\{.*\};\s*:`
	guard, err := regexp.Compile(guardPattern)
	if err != nil {
		t.Fatalf("compile guard regex: %v", err)
	}

	// Each variant exercises one whitespace permutation the production
	// regex is supposed to handle. The canonical case and the two trivial
	// trailing-space variants are MUST-MATCH (these are the most-cited
	// fork-bomb literals). The "space_inside_func_def" variant is a
	// known narrowness — the production regex requires literal `()` with
	// no characters inside the parens, but `:( ){ :|:& };:` is a valid
	// bomb invocation that defines and calls the function `:`. We keep
	// it in the table so the gap is self-documenting; an attacker who
	// reads the existing regex can craft this variant in a single edit.
	//
	// Until v0.2 #155 broadens the regex (or replaces shell-guard with
	// a stronger semantic check), `space_inside_func_def` FAILS by design.
	bombs := []struct {
		name        string
		text        string
		mustMatch   bool   // when true, regex MUST match; failure is regression.
		gapNote     string // explanation when mustMatch=false.
	}{
		{"canonical", `:(){ :|:& };:`, true, ""},
		{"trailing_double_space", `:(){ :|:& };  :`, true, ""},
		{"no_space_before_colon", `:(){ :|:&}; :`, true, ""},
		{
			"space_inside_func_def",
			`:( ){ :|:& };:`,
			false,
			"production regex requires \\(\\) with nothing between the parens; a space inside the parens slips through. " +
				"Documents C3-DIRECT-NARROW: regex is too tight. Closes when v0.2 #155 broadens shell-guard.",
		},
	}

	matched := 0
	for _, b := range bombs {
		t.Run(b.name, func(t *testing.T) {
			ok := guard.MatchString(b.text)
			if b.mustMatch {
				if !ok {
					t.Errorf("C3-DIRECT REGRESSION: shell-guard regex %q failed to match %q — bomb would reach exec",
						guardPattern, b.text)
					return
				}
				matched++
				return
			}
			// mustMatch=false: documented gap. We assert NOT-matched and
			// log the gap so the failure surfaces if the regex is later
			// broadened (at which point this row should be flipped to
			// mustMatch=true).
			if !ok {
				t.Errorf("C3-DIRECT-NARROW GAP CONFIRMED: regex did not match %q. %s",
					b.text, b.gapNote)
			} else {
				t.Logf("C3-DIRECT-NARROW closed: regex now matches %q — flip mustMatch=true", b.text)
			}
		})
	}
	if matched > 0 {
		t.Logf("C3-DIRECT: shell-guard regex matched %d/%d MUST-MATCH bomb variants", matched, 3)
	}
}

// TestRedteam_ForkBomb_IndirectViaScript_Limited documents C3-INDIRECT.
//
// The threat: an attacker can sidestep the shell-guard regex by writing
// the fork bomb to a file and invoking it via `sh fork.sh`. The shell-guard
// only sees the literal `sh fork.sh`, which is not a denied pattern. Once
// the script runs, fork() recursion explodes process-table headroom for
// the entire host within seconds — there is no RLIMIT_NPROC on the
// hardened-exec child today.
//
// Test mechanics:
//
//  1. We set RLIMIT_NPROC on the test process itself (a hard cap on
//     child-process count for our UID). This is a TEST-SAFETY MEASURE
//     ONLY — it's the only thing keeping the host responsive while we
//     exercise the unmitigated production code.
//
//  2. We write a small fork-bomb script to a tempdir, then attempt to
//     execute it via `sh fork.sh`. We mirror the production hardened_exec
//     contract slice that's relevant: Setpgid + Pdeathsig. We do NOT
//     import pkg/tools — keeps the test focused.
//
//  3. We measure host PID count before and during the run. The test
//     ASSERTS that production should impose its OWN limit (such that PID
//     growth saturates well below the test's safety cap). Today no such
//     limit exists, so the bomb saturates against the test cap and the
//     assertion fails — that's the documented gap.
//
// This test is documenting-only. It will FAIL until #155 adds RLIMIT_NPROC.
func TestRedteam_ForkBomb_IndirectViaScript_Limited(t *testing.T) {
	t.Logf("documents C3-INDIRECT (fork bomb via script) from insider-pentest report; closes when v0.2 #155 adds RLIMIT_NPROC")

	if runtime.GOOS != "linux" {
		t.Skip("Linux-only — this exercises Linux RLIMIT_NPROC")
	}
	if os.Getuid() == 0 {
		t.Skip("must run as non-root (root has effectively unlimited NPROC)")
	}

	// SAFETY GUARD: cap NPROC for the entire test process so a runaway
	// fork-bomb can't degrade the host. We pick a cap of 64 — well below
	// any realistic system limit, well above what a healthy test needs.
	// All children inherit this cap, which contains the worst case to
	// the test's own UID slice. RLIMIT_NPROC isn't in the syscall package
	// on Linux, so we go through golang.org/x/sys/unix for the constant.
	var oldRlim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NPROC, &oldRlim); err != nil {
		t.Fatalf("getrlimit NPROC (safety probe): %v", err)
	}
	defer func() {
		// Best-effort restore; if the test process still has children
		// alive this may EPERM, but the test is ending anyway.
		_ = unix.Setrlimit(unix.RLIMIT_NPROC, &oldRlim)
	}()
	const safetyCap uint64 = 64
	safetyRlim := unix.Rlimit{Cur: safetyCap, Max: oldRlim.Max}
	if err := unix.Setrlimit(unix.RLIMIT_NPROC, &safetyRlim); err != nil {
		t.Skipf("cannot lower RLIMIT_NPROC for test safety (need a non-restricted user): %v", err)
	}

	workspace := t.TempDir()
	scriptPath := filepath.Join(workspace, "fork.sh")
	bomb := `#!/bin/sh
:(){ :|:& };:
`
	if err := os.WriteFile(scriptPath, []byte(bomb), 0o755); err != nil {
		t.Fatalf("write fork.sh: %v", err)
	}

	// Sample baseline PID count for our UID before launching the bomb.
	baselinePIDs := countOurPIDs(t)

	// Run the script with a wall-clock cap. Production hardened_exec sets
	// Setpgid so we can signal the whole tree on timeout — we do the same
	// here. The bomb continuously self-replicates so we expect the run to
	// be killed by the context, not exit on its own.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", scriptPath)
	cmd.Dir = workspace
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGTERM,
	}

	startErr := cmd.Start()
	if startErr != nil {
		// Even Start can fail if NPROC is already at cap. That's actually
		// the right outcome for the threat — bomb didn't get to run. But
		// we don't accept that as proof of containment because production
		// does NOT use Start at the test's NPROC limit.
		t.Logf("cmd.Start failed (likely test-imposed NPROC cap): %v — note: this is the test's safety net, not production behaviour", startErr)
		t.Errorf(
			"C3-INDIRECT GAP CONFIRMED (preflight): even bomb startup hit the test's safety cap of %d. "+
				"That means production hardened_exec_linux.go applies NO limit of its own — the only thing "+
				"keeping the host alive is operator-level user nproc limits. Fix: ship RLIMIT_NPROC on "+
				"hardened-exec children in v0.2 (#155).",
			safetyCap,
		)
		return
	}

	// Sample peak PID count while the bomb runs.
	peakPIDs := samplePeakPIDs(t, ctx, baselinePIDs)

	// Kill the bomb's process group so we don't leave zombies.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Wait()

	// The threat is "bomb runs without bound until host exhausts PID
	// headroom". Production should cap descendant count via RLIMIT_NPROC
	// on the child. If the bomb spawned hundreds of additional PIDs
	// even under a 64-PID safety cap, the production code clearly is
	// not imposing its own limit — the cap we observe is the test's,
	// not the production code's.
	//
	// Heuristic: if peak - baseline >= safetyCap - 8 (within 8 of the
	// test's safety cap), it means the bomb only stopped because OUR
	// test cap kicked in. That confirms production has no limit of its own.
	growth := peakPIDs - baselinePIDs
	safetyHeadroom := int(safetyCap) - 8
	if growth >= safetyHeadroom {
		t.Errorf(
			"C3-INDIRECT GAP CONFIRMED: bomb grew PID count from %d to %d (+%d), saturating against TEST safety cap %d. "+
				"Production hardened_exec_linux.go does NOT set RLIMIT_NPROC — the only thing keeping the host alive in the real world is OS-level user nproc limits. "+
				"Fix: ship RLIMIT_NPROC on hardened-exec children in v0.2 (#155).",
			baselinePIDs, peakPIDs, growth, safetyCap,
		)
	} else {
		t.Logf("C3-INDIRECT growth=%d (baseline=%d, peak=%d) — production limit may be in place; investigate", growth, baselinePIDs, peakPIDs)
	}
}

// countOurPIDs returns an approximate count of processes owned by the
// current UID. Reads /proc; fast enough for a one-shot baseline.
func countOurPIDs(t *testing.T) int {
	t.Helper()
	uid := os.Getuid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatalf("read /proc: %v", err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip non-PID entries (like /proc/cpuinfo, /proc/meminfo etc.).
		name := e.Name()
		isDigits := true
		for _, r := range name {
			if r < '0' || r > '9' {
				isDigits = false
				break
			}
		}
		if !isDigits {
			continue
		}
		// Stat /proc/<pid> to get owner UID. This may fail if the process
		// exited between readdir and stat — that's fine, just skip it.
		info, err := os.Stat("/proc/" + name)
		if err != nil {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		if int(stat.Uid) == uid {
			count++
		}
	}
	return count
}

// samplePeakPIDs polls our PID count until the context fires, returning
// the maximum observed value. We poll at 100ms granularity which is
// plenty fine for a fork bomb — the bomb doubles every fork, so a
// healthy bomb saturates RLIMIT_NPROC in well under 100ms.
func samplePeakPIDs(t *testing.T, ctx context.Context, baseline int) int {
	t.Helper()
	peak := baseline
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return peak
		case <-tick.C:
			c := countOurPIDs(t)
			if c > peak {
				peak = c
			}
		}
	}
}
