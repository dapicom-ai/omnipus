// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

//go:build !windows

package fileutil

import (
	"errors"
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
//
// Errors from fn, flockUnlock, and f.Close are all captured and joined so
// none are silently discarded.
func WithFlock(path string, fn func() error) (retErr error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("fileutil: open for flock %q: %w", path, err)
	}
	// Defer close so it always runs; capture its error and join with retErr.
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("fileutil: close flock %q: %w", path, closeErr))
		}
	}()

	if err := flockExclusive(f); err != nil {
		return fmt.Errorf("fileutil: acquire flock %q: %w", path, err)
	}
	// Defer unlock so it always runs after fn; capture its error and join with retErr.
	defer func() {
		if unlockErr := flockUnlock(f); unlockErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("fileutil: release flock %q: %w", path, unlockErr))
		}
	}()

	return fn()
}
