package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newUnifiedStoreForTest(t *testing.T) *UnifiedStore {
	t.Helper()
	store, err := NewUnifiedStore(t.TempDir())
	require.NoError(t, err, "NewUnifiedStore must succeed")
	return store
}

func createSessionFile(t *testing.T, store *UnifiedStore, sessionID, filename string, age time.Duration) string {
	t.Helper()
	sessionDir := filepath.Join(store.baseDir, sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0o700))
	filePath := filepath.Join(sessionDir, filename)
	require.NoError(t, os.WriteFile(filePath, []byte(`{"id":"test"}`+"\n"), 0o600))
	mtime := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(filePath, mtime, mtime))
	return filePath
}

// TestRetentionSweep_DeletesAgedFiles creates 3 session files aged 3/10/30 days,
// calls RetentionSweep(7), and asserts that only the 3-day file survives.
func TestRetentionSweep_DeletesAgedFiles(t *testing.T) {
	store := newUnifiedStoreForTest(t)

	recent := createSessionFile(t, store, "sess-a", "2026-04-20.jsonl", 3*24*time.Hour)
	stale1 := createSessionFile(t, store, "sess-b", "2026-04-12.jsonl", 10*24*time.Hour)
	stale2 := createSessionFile(t, store, "sess-c", "2026-03-23.jsonl", 30*24*time.Hour)

	removed, err := store.RetentionSweep(7)
	require.NoError(t, err)
	assert.Equal(t, 2, removed, "two files older than 7 days must be deleted")

	_, err = os.Stat(recent)
	assert.NoError(t, err, "recent file (3 days old) must survive RetentionSweep(7)")

	_, err = os.Stat(stale1)
	assert.True(t, os.IsNotExist(err), "10-day-old file must be deleted")

	_, err = os.Stat(stale2)
	assert.True(t, os.IsNotExist(err), "30-day-old file must be deleted")
}

// TestRetentionSweep_ZeroRetentionIsNoOp verifies that retentionDays <= 0
// is a no-op that returns (0, nil) without touching any file.
func TestRetentionSweep_ZeroRetentionIsNoOp(t *testing.T) {
	store := newUnifiedStoreForTest(t)

	filePath := createSessionFile(t, store, "sess-x", "2025-01-01.jsonl", 365*24*time.Hour)

	removed, err := store.RetentionSweep(0)
	require.NoError(t, err)
	assert.Equal(t, 0, removed, "RetentionSweep(0) must be a no-op")

	_, statErr := os.Stat(filePath)
	assert.NoError(t, statErr, "file must not be deleted when retentionDays == 0")
}

// TestRetentionSweep_EmptyStore verifies that sweeping an empty store returns (0, nil).
func TestRetentionSweep_EmptyStore(t *testing.T) {
	store := newUnifiedStoreForTest(t)

	removed, err := store.RetentionSweep(7)
	require.NoError(t, err)
	assert.Equal(t, 0, removed, "empty store must return 0 removed files")
}

// TestRetentionSweep_PartialDeleteFailureContinues verifies that when one file
// cannot be deleted the sweep continues and processes remaining files.
//
// We simulate an undeletable file by replacing a target .jsonl path with a
// directory of the same name (os.Remove on a non-empty directory fails).
func TestRetentionSweep_PartialDeleteFailureContinues(t *testing.T) {
	store := newUnifiedStoreForTest(t)

	// Create a stale file that can be deleted.
	deletable := createSessionFile(t, store, "sess-del", "2026-01-01.jsonl", 30*24*time.Hour)

	// Simulate an undeletable file: create a session directory, then put a
	// sub-directory where the .jsonl file would be so os.Remove fails.
	sessionDir := filepath.Join(store.baseDir, "sess-nodeleate")
	require.NoError(t, os.MkdirAll(sessionDir, 0o700))
	fakePath := filepath.Join(sessionDir, "2026-01-01.jsonl")
	require.NoError(t, os.MkdirAll(fakePath, 0o700)) // directory, not a file
	// Backdate the directory so WalkDir sees it as stale; we need its DirEntry to
	// report a .jsonl suffix — WalkDir reports the name of the entry, so this works.
	mtime := time.Now().Add(-30 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(fakePath, mtime, mtime))

	// Put a child inside so os.Remove fails with "directory not empty".
	require.NoError(t, os.WriteFile(filepath.Join(fakePath, "child"), []byte("x"), 0o600))

	removed, err := store.RetentionSweep(7)
	require.NoError(t, err, "sweep must not abort on a partial delete failure")

	// The deletable file must be gone.
	_, statErr := os.Stat(deletable)
	assert.True(t, os.IsNotExist(statErr), "deletable file must be removed")

	// removed may be 1 (only the file that succeeded).
	assert.Equal(t, 1, removed, "only the successfully deleted file must be counted")
}
