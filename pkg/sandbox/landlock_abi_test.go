// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLandlockABI4IssueRef_SingleConstant asserts the exported constant
// has the expected value (Sprint-K k07) and that no other non-test Go file
// under pkg/ or cmd/ hardcodes the literal "#138" outside this file.
func TestLandlockABI4IssueRef_SingleConstant(t *testing.T) {
	if LandlockABI4IssueRef != "#138" {
		t.Fatalf("LandlockABI4IssueRef = %q, want %q", LandlockABI4IssueRef, "#138")
	}

	repoRoot := findRepoRoot(t)
	roots := []string{
		filepath.Join(repoRoot, "pkg"),
		filepath.Join(repoRoot, "cmd"),
	}
	needle := `"#138"`
	// landlock_abi.go defines the constant — the only allowed hardcoded
	// occurrence. Every other Go file under pkg/ or cmd/ must reference
	// the constant instead of the literal.
	allowedFile := "landlock_abi.go"

	var hits []string
	for _, root := range roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if filepath.Base(path) == allowedFile {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(data), needle) {
				hits = append(hits, path)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", root, walkErr)
		}
	}

	if len(hits) > 0 {
		t.Errorf("literal %s found outside %s — reference sandbox.LandlockABI4IssueRef instead.\nHits:\n  %s",
			needle, allowedFile, strings.Join(hits, "\n  "))
	}
}

// TestLandlockABIProbeResult_IssueRefPopulated verifies that Status.IssueRef
// is populated with LandlockABI4IssueRef when abi_version >= 4, and empty
// otherwise. This is the wire contract consumed by the /sandbox-status UI.
func TestLandlockABIProbeResult_IssueRefPopulated(t *testing.T) {
	cases := []struct {
		name       string
		abiVersion int
		want       string
	}{
		{"abi_v3_no_issue", 3, ""},
		{"abi_v4_issue_populated", 4, "#138"},
		{"abi_v5_issue_populated", 5, "#138"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockABIBackend{abiVersion: tc.abiVersion, applied: false}
			status := DescribeBackend(mock)
			if status.IssueRef != tc.want {
				t.Errorf("abi_version=%d: IssueRef = %q, want %q",
					tc.abiVersion, status.IssueRef, tc.want)
			}
		})
	}
}

// findRepoRoot walks up from the test file's directory until it finds
// go.mod, returning that directory. The test file sits in pkg/sandbox/ so
// two levels up gives the repo root in the expected layout.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate go.mod walking up from test file")
	return ""
}
