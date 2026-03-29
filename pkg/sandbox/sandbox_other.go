//go:build !linux

package sandbox

import "log/slog"

func selectBackendPlatform() (SandboxBackend, string) {
	slog.Info("Kernel sandbox not available on this platform. Using application-level enforcement.",
		"backend", "fallback")
	fb := NewFallbackBackend()
	return fb, fb.Name()
}

func probeLandlockABIPlatform() int {
	return 0
}
