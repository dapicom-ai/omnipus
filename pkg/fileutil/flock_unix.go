// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

//go:build !windows

package fileutil

import (
	"os"

	"golang.org/x/sys/unix"
)

func flockExclusive(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

func flockUnlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
