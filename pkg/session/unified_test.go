// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Tests for UnifiedStore.DeleteSession — Milestone 2.
//
// BDD scenarios:
//   Scenario: Delete existing session — verify directory removal
//   Scenario: Delete non-existent session — verify "not found" error
//   Scenario: Path traversal rejected — "../evil" returns validation error
//   Scenario: Empty session ID rejected — returns validation error
//   Scenario: ".." session ID rejected — returns validation error
//
// Traces to: pkg/session/unified.go — DeleteSession method (Milestone 2)

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStore creates a UnifiedStore rooted at t.TempDir() and returns it.
// Callers are responsible for closing it if needed.
func newTestStore(t *testing.T) *UnifiedStore {
	t.Helper()
	store, err := NewUnifiedStore(t.TempDir())
	require.NoError(t, err, "NewUnifiedStore must succeed")
	return store
}

// TestDeleteSession_Success creates a session, verifies the directory exists,
// deletes it, and asserts the directory is gone.
//
// BDD: Given a session has been created,
// When DeleteSession is called with its ID,
// Then no error is returned AND the session directory no longer exists.
//
// Traces to: pkg/session/unified.go DeleteSession (Milestone 2)
func TestDeleteSession_Success(t *testing.T) {
	store := newTestStore(t)

	// Given — create a real session.
	meta, err := store.NewSession(SessionTypeChat, "", "test-agent")
	require.NoError(t, err, "NewSession must succeed")
	sessionID := meta.ID

	// Verify the directory was actually created (precondition for a meaningful test).
	sessionDir := filepath.Join(store.baseDir, sessionID)
	_, statErr := os.Stat(sessionDir)
	require.NoError(t, statErr, "session directory must exist before deletion")

	// When — delete the session.
	err = store.DeleteSession(sessionID)

	// Then — no error and directory is gone.
	require.NoError(t, err, "DeleteSession must succeed for an existing session")
	_, statErr = os.Stat(sessionDir)
	assert.True(t, os.IsNotExist(statErr),
		"session directory must be removed after DeleteSession; stat error: %v", statErr)
}

// TestDeleteSession_DifferentSessions verifies that deleting one session does not
// affect another — proving DeleteSession operates on the targeted directory only.
//
// This is the differentiation test: two sessions, delete one, verify the other survives.
//
// Traces to: pkg/session/unified.go DeleteSession (Milestone 2)
func TestDeleteSession_DifferentSessions(t *testing.T) {
	store := newTestStore(t)

	// Create two sessions.
	meta1, err := store.NewSession(SessionTypeChat, "", "test-agent")
	require.NoError(t, err)
	meta2, err := store.NewSession(SessionTypeChat, "", "test-agent")
	require.NoError(t, err)

	id1 := meta1.ID
	id2 := meta2.ID
	require.NotEqual(t, id1, id2, "two sessions must have distinct IDs")

	dir2 := filepath.Join(store.baseDir, id2)

	// Delete session 1.
	require.NoError(t, store.DeleteSession(id1), "DeleteSession(id1) must succeed")

	// Session 1's dir must be gone.
	_, statErr := os.Stat(filepath.Join(store.baseDir, id1))
	assert.True(t, os.IsNotExist(statErr), "deleted session directory must not exist")

	// Session 2's dir must still be present.
	_, statErr = os.Stat(dir2)
	assert.NoError(t, statErr, "non-deleted session directory must still exist")
}

// TestDeleteSession_NotFound verifies that deleting a non-existent session ID
// returns an error containing "not found".
//
// BDD: Given no session with ID "does-not-exist" exists,
// When DeleteSession("does-not-exist") is called,
// Then an error is returned containing "not found".
//
// Traces to: pkg/session/unified.go DeleteSession (Milestone 2)
func TestDeleteSession_NotFound(t *testing.T) {
	store := newTestStore(t)

	err := store.DeleteSession("does-not-exist-session-id")

	require.Error(t, err, "DeleteSession must return an error for a non-existent session")
	assert.True(t, strings.Contains(err.Error(), "not found"),
		"error must contain 'not found', got: %q", err.Error())
}

// TestDeleteSession_PathTraversal verifies that attempting to delete "../evil"
// is rejected with a validation error before any filesystem operation.
//
// BDD: Given a malicious session ID "../evil",
// When DeleteSession("../evil") is called,
// Then an error is returned about invalid session ID (validateSessionID rejects it).
//
// Traces to: pkg/session/unified.go validateSessionID (Milestone 2)
func TestDeleteSession_PathTraversal(t *testing.T) {
	store := newTestStore(t)

	err := store.DeleteSession("../evil")

	require.Error(t, err, "DeleteSession must reject path-traversal session IDs")
	// The error must come from validateSessionID, not from a filesystem operation
	// that traversed out of the base directory.
	assert.Contains(t, err.Error(), "invalid session ID",
		"error must mention 'invalid session ID', got: %q", err.Error())
}

// TestDeleteSession_EmptyID verifies that an empty string session ID is rejected.
//
// BDD: Given an empty session ID "",
// When DeleteSession("") is called,
// Then an error is returned about invalid session ID.
//
// Traces to: pkg/session/unified.go validateSessionID (Milestone 2)
func TestDeleteSession_EmptyID(t *testing.T) {
	store := newTestStore(t)

	err := store.DeleteSession("")

	require.Error(t, err, "DeleteSession must reject empty session ID")
	assert.Contains(t, err.Error(), "invalid session ID",
		"error must mention 'invalid session ID', got: %q", err.Error())
}

// TestDeleteSession_DoubleDot verifies that ".." as a session ID is rejected.
//
// BDD: Given session ID "..",
// When DeleteSession("..") is called,
// Then an error is returned about invalid session ID.
//
// Traces to: pkg/session/unified.go validateSessionID (Milestone 2)
func TestDeleteSession_DoubleDot(t *testing.T) {
	store := newTestStore(t)

	err := store.DeleteSession("..")

	require.Error(t, err, "DeleteSession must reject '..' as session ID")
	assert.Contains(t, err.Error(), "invalid session ID",
		"error must mention 'invalid session ID', got: %q", err.Error())
}

// TestDeleteSession_PersistenceCheck verifies the read-back contract:
// after deletion, GetMeta must fail.
//
// This is the persistence test: ensures deletion is durable and not superficial.
//
// Traces to: pkg/session/unified.go DeleteSession + GetMeta (Milestone 2)
func TestDeleteSession_PersistenceCheck(t *testing.T) {
	store := newTestStore(t)

	// Create, verify readable, delete, verify unreadable.
	meta, err := store.NewSession(SessionTypeChat, "", "test-agent")
	require.NoError(t, err)

	// Read before deletion — must succeed.
	_, err = store.GetMeta(meta.ID)
	require.NoError(t, err, "GetMeta must succeed before deletion")

	// Delete.
	require.NoError(t, store.DeleteSession(meta.ID))

	// Read after deletion — must fail.
	_, err = store.GetMeta(meta.ID)
	assert.Error(t, err, "GetMeta must return error after session is deleted")
}
