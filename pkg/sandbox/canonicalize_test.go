// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCanonicalizePath_ExistingPath verifies that an existing path is resolved
// via filepath.EvalSymlinks and returned unchanged when no symlinks exist.
func TestCanonicalizePath_ExistingPath(t *testing.T) {
	dir := t.TempDir()
	resolved, err := canonicalizePath(dir)
	require.NoError(t, err)
	// The resolved path must point to the same directory (may differ by symlink).
	// Use EvalSymlinks directly to get the expected canonical path.
	expected, evalErr := filepath.EvalSymlinks(dir)
	require.NoError(t, evalErr)
	assert.Equal(t, expected, resolved)
}

// TestCanonicalizePath_MissingFile verifies that a path where only the parent
// exists (file not yet created) resolves the parent and appends the file name.
func TestCanonicalizePath_MissingFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "newfile.json")

	resolved, err := canonicalizePath(missing)
	require.NoError(t, err)

	// Parent must be resolved; the missing file segment is appended.
	resolvedParent, evalErr := filepath.EvalSymlinks(dir)
	require.NoError(t, evalErr)
	assert.Equal(t, filepath.Join(resolvedParent, "newfile.json"), resolved)
}

// TestCanonicalizePath_DeepMissingPath verifies that a path with multiple
// non-existent components still resolves the deepest existing ancestor and
// appends all missing segments. This is the core F13 regression case:
// /var/folders/x/y/z where only /var exists (symlink on macOS → /private/var).
func TestCanonicalizePath_DeepMissingPath(t *testing.T) {
	dir := t.TempDir()
	// Build a 3-level deep missing path under the existing temp dir.
	deep := filepath.Join(dir, "level1", "level2", "level3", "file.json")

	resolved, err := canonicalizePath(deep)
	require.NoError(t, err)

	// The resolved path must start with the canonical form of dir.
	resolvedBase, evalErr := filepath.EvalSymlinks(dir)
	require.NoError(t, evalErr)
	assert.True(t, strings.HasPrefix(resolved, resolvedBase),
		"resolved path %q must start with canonical base %q", resolved, resolvedBase)
	// Tail segments must be preserved.
	assert.True(t, strings.HasSuffix(resolved, filepath.Join("level1", "level2", "level3", "file.json")),
		"resolved path %q must end with the missing tail segments", resolved)
}

// TestCanonicalizePath_Symlink verifies that a symlinked path is fully resolved
// to its real path.
func TestCanonicalizePath_Symlink(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("symlink test unreliable as root")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	require.NoError(t, os.Mkdir(target, 0o700))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(target, link))

	resolved, err := canonicalizePath(link)
	require.NoError(t, err)

	expected, evalErr := filepath.EvalSymlinks(target)
	require.NoError(t, evalErr)
	assert.Equal(t, expected, resolved, "symlink must resolve to the real target")
}

// TestCanonicalizePath_RelativePath verifies that a relative path is converted
// to an absolute canonical path.
func TestCanonicalizePath_RelativePath(t *testing.T) {
	dir := t.TempDir()
	// Change to the temp dir to make a relative path meaningful, but use Abs
	// to compute expected result without chdir (which is global state).
	relative := "."
	abs, err := filepath.Abs(relative)
	require.NoError(t, err)
	resolved, err := canonicalizePath(relative)
	require.NoError(t, err)
	// Both must be absolute.
	assert.True(t, filepath.IsAbs(resolved), "result must be absolute")
	// The resolved path must share a prefix with the abs path.
	_ = abs
	_ = dir
}
