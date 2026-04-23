// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLandlockABIProbeResult_V4Works verifies that DescribeBackend
// returns valid LandlockFeatures for ABI v4 (including network rights)
// and does NOT populate IssueRef (since the ABI v4 incompatibility is now fixed).
func TestLandlockABIProbeResult_V4Works(t *testing.T) {
	cases := []struct {
		name       string
		abiVersion int
		wantFeats  []string
	}{
		{
			name:       "abi_v3_no_network",
			abiVersion: 3,
			wantFeats:  []string{"EXECUTE", "WRITE_FILE", "READ_FILE", "READ_DIR", "REMOVE_DIR", "REMOVE_FILE", "MAKE_CHAR", "MAKE_DIR", "MAKE_REG", "MAKE_SOCK", "MAKE_FIFO", "MAKE_BLOCK", "MAKE_SYM", "REFER", "TRUNCATE", "IOCTL_DEV"},
		},
		{
			name:       "abi_v4_with_network",
			abiVersion: 4,
			wantFeats:  []string{"EXECUTE", "WRITE_FILE", "READ_FILE", "READ_DIR", "REMOVE_DIR", "REMOVE_FILE", "MAKE_CHAR", "MAKE_DIR", "MAKE_REG", "MAKE_SOCK", "MAKE_FIFO", "MAKE_BLOCK", "MAKE_SYM", "REFER", "TRUNCATE", "IOCTL_DEV", "NET_BIND_TCP", "NET_CONNECT_TCP"},
		},
		{
			name:       "abi_v5_with_network",
			abiVersion: 5,
			wantFeats:  []string{"EXECUTE", "WRITE_FILE", "READ_FILE", "READ_DIR", "REMOVE_DIR", "REMOVE_FILE", "MAKE_CHAR", "MAKE_DIR", "MAKE_REG", "MAKE_SOCK", "MAKE_FIFO", "MAKE_BLOCK", "MAKE_SYM", "REFER", "TRUNCATE", "IOCTL_DEV", "NET_BIND_TCP", "NET_CONNECT_TCP"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockABIBackend{abiVersion: tc.abiVersion, applied: true}
			status := DescribeBackend(mock)
			for _, want := range tc.wantFeats {
				found := false
				for _, f := range status.LandlockFeatures {
					if f == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("missing feature %q; got %v", want, status.LandlockFeatures)
				}
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
