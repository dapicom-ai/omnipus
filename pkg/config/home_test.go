// Omnipus - Ultra-lightweight personal AI agent
// License: MIT

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOmnipusHomeDir_HonorsEnvOverride(t *testing.T) {
	t.Setenv(EnvHome, "/opt/custom-omnipus")

	got := OmnipusHomeDir()
	if got != "/opt/custom-omnipus" {
		t.Errorf("OmnipusHomeDir() with env override = %q, want %q", got, "/opt/custom-omnipus")
	}
}

func TestOmnipusHomeDir_FallsBackToHomeDirDotOmnipus(t *testing.T) {
	t.Setenv(EnvHome, "")

	got := OmnipusHomeDir()
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("UserHomeDir failed on this host; skipping: %v", err)
	}
	want := filepath.Join(userHome, ".omnipus")
	if got != want {
		t.Errorf("OmnipusHomeDir() with empty env = %q, want %q", got, want)
	}
}

func TestOmnipusHomeDir_EmptyEnvIsTreatedAsUnset(t *testing.T) {
	// An empty OMNIPUS_HOME must NOT be returned verbatim — otherwise callers
	// that join ".omnipus" onto it would end up writing to the process cwd.
	t.Setenv(EnvHome, "")

	got := OmnipusHomeDir()
	if got == "" {
		t.Fatal("OmnipusHomeDir() returned empty string for empty env; expected fallback")
	}
	if !strings.Contains(got, ".omnipus") && !strings.Contains(got, "omnipus-") {
		t.Errorf("OmnipusHomeDir() fallback = %q, expected it to contain \".omnipus\" or a temp prefix", got)
	}
}
