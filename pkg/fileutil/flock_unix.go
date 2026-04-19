// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

//go:build !windows

package fileutil

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func flockExclusive(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

func flockUnlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}

// WithFlock acquires an OS-level advisory write lock on path before calling fn,
// then releases it. This is defense-in-depth alongside single-writer goroutines
// for shared files (config.json, credentials.json).
//
// On Windows this function is provided by flock_windows.go and calls fn
// directly without opening the file, because an open handle on the destination
// file prevents WriteFileAtomic from renaming the temp file over it.
func WithFlock(path string, fn func() error) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("fileutil: open for flock %q: %w", path, err)
	}
	defer f.Close()

	if err := flockExclusive(f); err != nil {
		return fmt.Errorf("fileutil: acquire flock %q: %w", path, err)
	}
	defer flockUnlock(f) //nolint:errcheck

	return fn()
}
