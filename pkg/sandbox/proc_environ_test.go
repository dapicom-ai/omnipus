//go:build linux

// T2.7: ChildCannotReadGatewayProcEnviron.
//
// Documents gap C6 from the pentest report: a hardened child process can read
// /proc/<parent-pid>/environ because Linux grants same-uid processes read access
// to /proc/<pid>/environ and the production Landlock policy does not restrict /proc.
//
// THIS TEST IS EXPECTED TO FAIL TODAY — it documents the gap.
// It will flip to a positive test once the v0.2 #155 fix (restricting /proc/self/.. or
// per-process namespace isolation) lands.

package sandbox_test

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
)

// TestChildCannotReadGatewayProcEnviron (T2.7) spawns a subprocess that
// attempts to read /proc/<parent-pid>/environ (the test process acting as
// the "gateway" parent). The test asserts EACCES or EPERM from the kernel.
//
// Expected outcome today: FAIL (the child CAN read the parent's environ).
// Expected outcome after v0.2 C6 fix: PASS.
//
// The test is structured as a subprocess re-exec using the same env-sentinel
// pattern as TestLandlock_ApplySubprocess to avoid permanently restricting
// the parent test process.
func TestChildCannotReadGatewayProcEnviron(t *testing.T) {
	t.Logf("documents C6 from pentest report: same-uid /proc/<pid>/environ readable by children")

	if os.Getenv("OMNIPUS_PROC_ENVIRON_CHILD") == "1" {
		runProcEnvironChild()
		return // unreachable — runProcEnvironChild calls os.Exit
	}

	if os.Getuid() == 0 {
		t.Skip("proc-environ test must run as non-root (root can always read /proc)")
	}

	parentPID := os.Getpid()

	//nolint:gosec // intentional test-binary self-exec
	cmd := exec.Command(os.Args[0],
		"-test.run=TestChildCannotReadGatewayProcEnviron",
		"-test.count=1",
		"-test.v",
	)
	cmd.Env = append(os.Environ(),
		"OMNIPUS_PROC_ENVIRON_CHILD=1",
		fmt.Sprintf("OMNIPUS_PROC_ENVIRON_PARENT_PID=%d", parentPID),
	)
	// Use a process group so child cleanup is clean.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	out, err := cmd.CombinedOutput()
	var exitCode int
	if err == nil {
		exitCode = 0
	} else if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else {
		t.Fatalf("child process failed to run: %v\n%s", err, out)
	}

	switch exitCode {
	case 0:
		// Child reported EACCES/EPERM — gap is fixed.
		t.Logf("C6 gap is FIXED: child could not read parent's /proc/environ (child exited 0)")
	case 1:
		// Child successfully read the parent's environ — gap is still open.
		// This is the expected outcome today; log clearly so CI shows the documented gap.
		t.Logf("C6 gap OPEN (expected today): child read parent /proc/%d/environ\n%s",
			parentPID, out)
		t.Logf("documents C6 from pentest report: /proc/<pid>/environ readable by same-uid children")
		// Mark as expected failure rather than t.Fatal so the test suite documents it.
		t.Fail()
	case 77:
		t.Skipf("/proc not mounted or parent PID not passed (child exit 77):\n%s", out)
	default:
		t.Fatalf("child exit %d (expected 0=blocked, 1=readable gap, 77=skip):\n%s",
			exitCode, out)
	}
}

// runProcEnvironChild is the child-mode implementation. Communicates via exit codes:
//
//	0  — could NOT read parent environ (EACCES/EPERM) — gap is fixed
//	1  — successfully read parent environ — gap is still open
//	77 — /proc unavailable or parent PID missing
func runProcEnvironChild() {
	pidStr := os.Getenv("OMNIPUS_PROC_ENVIRON_PARENT_PID")
	if pidStr == "" {
		os.Exit(77)
	}
	parentPID, err := strconv.Atoi(pidStr)
	if err != nil || parentPID <= 0 {
		fmt.Fprintf(os.Stderr, "invalid OMNIPUS_PROC_ENVIRON_PARENT_PID=%q\n", pidStr)
		os.Exit(77)
	}

	path := fmt.Sprintf("/proc/%d/environ", parentPID)
	_, readErr := os.ReadFile(path)
	if readErr == nil {
		// Successfully read — gap is still open (documented failure).
		fmt.Fprintf(os.Stderr, "C6: child read %s successfully — gap still open\n", path)
		os.Exit(1)
	}

	// Check for EACCES or EPERM — the expected outcome after the fix.
	if isEACCESErr(readErr) {
		fmt.Fprintf(os.Stderr, "C6: child got permission denied on %s — gap fixed: %v\n", path, readErr)
		os.Exit(0)
	}

	// Some other error (ENOENT means the parent exited before child ran).
	if os.IsNotExist(readErr) {
		fmt.Fprintf(os.Stderr, "parent PID %d no longer exists; skip\n", parentPID)
		os.Exit(77)
	}
	fmt.Fprintf(os.Stderr, "unexpected error reading %s: %v\n", path, readErr)
	os.Exit(2)
}

func isEACCESErr(err error) bool {
	return err != nil && (os.IsPermission(err))
}
