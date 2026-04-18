package testutil

import (
	"runtime"
	"testing"
)

// SkipIfNotLinux skips the test when GOOS != "linux".
// Use for tests that exercise Linux-specific facilities (Landlock, seccomp,
// /proc introspection, AF_NETLINK, etc.).
func SkipIfNotLinux(tb testing.TB) {
	tb.Helper()
	if runtime.GOOS != "linux" {
		tb.Skipf("skipping: test requires Linux, running on %s", runtime.GOOS)
	}
}

// SkipIfNotAmd64 skips the test when GOARCH != "amd64".
func SkipIfNotAmd64(tb testing.TB) {
	tb.Helper()
	if runtime.GOARCH != "amd64" {
		tb.Skipf("skipping: test requires amd64, running on %s", runtime.GOARCH)
	}
}

// SkipIfNotLinuxAmd64 is the conjunction of SkipIfNotLinux and SkipIfNotAmd64.
func SkipIfNotLinuxAmd64(tb testing.TB) {
	tb.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		tb.Skipf("skipping: test requires linux/amd64, running on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

// SkipOnWindows skips the test on Windows. Use for path-semantics tests
// where Windows has legitimately different behavior (e.g., no "/" vs "\"
// distinction for our purposes).
func SkipOnWindows(tb testing.TB) {
	tb.Helper()
	if runtime.GOOS == "windows" {
		tb.Skip("skipping: test is not applicable on Windows due to path semantics differences")
	}
}

// SkipOnDarwin skips the test on macOS.
func SkipOnDarwin(tb testing.TB) {
	tb.Helper()
	if runtime.GOOS == "darwin" {
		tb.Skip("skipping: test is not applicable on macOS/Darwin")
	}
}

// SkipIfCGONotAvailable skips when CGO is disabled at build time. Use for
// tests that genuinely require cgo (e.g., race detector, sqlite/whatsmeow
// in some deployments).
//
// Detection is done via a build-tag-guarded constant defined in
// platform_detect_cgo.go (cgo enabled) and platform_detect_nocgo.go (cgo
// disabled). This avoids importing "C" in the file that defines the skip
// helper.
func SkipIfCGONotAvailable(tb testing.TB) {
	tb.Helper()
	if !cgoEnabled {
		tb.Skip("skipping: test requires CGO, but binary was built with CGO_ENABLED=0")
	}
}

// SkipIfShortMode skips the test when -short is passed to go test. The reason
// parameter should describe why the test is expensive (e.g., "runs a full
// load ramp lasting 30 s"). This is a thin wrapper around testing.Short() so
// test authors surface a concrete reason consistently.
func SkipIfShortMode(tb testing.TB, reason string) {
	tb.Helper()
	if testing.Short() {
		tb.Skipf("skipping in short mode: %s", reason)
	}
}
