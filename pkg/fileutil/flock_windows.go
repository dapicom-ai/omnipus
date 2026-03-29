// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

//go:build windows

package fileutil

import "os"

// On Windows we rely on the single-writer goroutine pattern; LockFileEx
// integration is deferred to a future wave. fn is called without OS-level
// locking as graceful degradation per hard constraint 4.
func flockExclusive(_ *os.File) error { return nil }
func flockUnlock(_ *os.File) error    { return nil }
