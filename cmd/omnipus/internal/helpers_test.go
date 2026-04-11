package internal

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

func TestGetConfigPath(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")

	got := GetConfigPath()
	want := filepath.Join("/tmp/home", ".omnipus", "config.json")

	assert.Equal(t, want, got)
}

func TestGetConfigPath_WithOMNIPUS_HOME(t *testing.T) {
	t.Setenv(config.EnvHome, "/custom/omnipus")
	t.Setenv("HOME", "/tmp/home")

	got := GetConfigPath()
	want := filepath.Join("/custom/omnipus", "config.json")

	assert.Equal(t, want, got)
}

func TestGetConfigPath_WithOMNIPUS_CONFIG(t *testing.T) {
	t.Setenv("OMNIPUS_CONFIG", "/custom/config.json")
	t.Setenv(config.EnvHome, "/custom/omnipus")
	t.Setenv("HOME", "/tmp/home")

	got := GetConfigPath()
	want := "/custom/config.json"

	assert.Equal(t, want, got)
}

func TestGetConfigPath_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific HOME behavior varies; run on windows")
	}

	testUserProfilePath := `C:\Users\Test`
	t.Setenv("USERPROFILE", testUserProfilePath)

	got := GetConfigPath()
	want := filepath.Join(testUserProfilePath, ".omnipus", "config.json")

	require.True(t, strings.EqualFold(got, want), "GetConfigPath() = %q, want %q", got, want)
}
