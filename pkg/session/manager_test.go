package session

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"simple"},
		{"telegram:123456"},
		{"discord:987654321"},
		{"slack:C01234"},
		{"no-colons-here"},
		{"multiple:colons:here"},
		{"agent:main:telegram:group:-1003822706455/12"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			want := hex.EncodeToString([]byte(tt.input))
			if got != want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, want)
			}
		})
	}
}

func TestSave_WithColonInKey(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSessionManager(tmpDir)

	// Create a session with a key containing colon (typical channel session key).
	key := "telegram:123456"
	sm.GetOrCreate(key)
	sm.AddMessage(key, "user", "hello")

	// Save should succeed even though the key contains ':'.
	if err := sm.Save(key); err != nil {
		t.Fatalf("Save(%q) failed: %v", key, err)
	}

	// The file on disk should use hex-encoded name.
	expectedFile := filepath.Join(tmpDir, hex.EncodeToString([]byte(key))+".json")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Fatalf("expected session file %s to exist", expectedFile)
	}

	// Load into a fresh manager and verify the session round-trips.
	sm2 := NewSessionManager(tmpDir)
	history := sm2.GetHistory(key)
	if len(history) != 1 {
		t.Fatalf("expected 1 message after reload, got %d", len(history))
	}
	if history[0].Content != "hello" {
		t.Errorf("expected message content %q, got %q", "hello", history[0].Content)
	}
}

func TestSave_RejectsPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSessionManager(tmpDir)

	// Invalid raw keys that must be rejected before encoding.
	badKeys := []string{"", ".", ".."}
	for _, key := range badKeys {
		sm.GetOrCreate(key)
		if err := sm.Save(key); err == nil {
			t.Errorf("Save(%q) should have failed but didn't", key)
		}
	}

	// Keys containing path separators are hex-encoded (no subdirs created).
	sm.GetOrCreate("foo/bar")
	if err := sm.Save("foo/bar"); err != nil {
		t.Fatalf("Save(\"foo/bar\") after sanitize should succeed: %v", err)
	}
	expectedHex := hex.EncodeToString([]byte("foo/bar"))
	if _, err := os.Stat(filepath.Join(tmpDir, expectedHex+".json")); os.IsNotExist(err) {
		t.Errorf("expected %s.json in storage (hex-encoded from foo/bar)", expectedHex)
	}
}
