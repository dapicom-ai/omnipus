// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

//go:build windows

package fileutil

import (
	"log/slog"
	"os"
	"sync"
)

// On Windows we rely on the single-writer goroutine pattern; LockFileEx
// integration is deferred to a future wave. fn is called without OS-level
// locking as graceful degradation per hard constraint 4.
func flockExclusive(_ *os.File) error { return nil }
func flockUnlock(_ *os.File) error    { return nil }

// warnOnce ensures the advisory-flock degradation warning is emitted at most
// once per process lifetime to avoid log spam on every WithFlock call.
var warnOnce sync.Once

// WithFlock on Windows calls fn directly without opening or locking the
// target file. Opening the file on Windows would leave a read/write handle
// open for the duration of fn, which prevents WriteFileAtomic from renaming
// the temp file over the destination (Windows denies rename when any handle is
// open on the destination file). The single-writer goroutine serialization in
// Store provides the required concurrency safety without OS-level locking.
//
// Per hard constraint 4 (graceful degradation), this is logged once at Warn
// level so operators are aware that OS-level advisory locking is inactive.
func WithFlock(path string, fn func() error) error {
	warnOnce.Do(func() {
		slog.Warn("fileutil: Windows lacks advisory flock; concurrent writers rely on single-writer goroutine only",
			"file", path)
	})
	return fn()
}
