// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package credentials

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// InjectFromConfig iterates over cfg.ModelList entries, reads each entry's
// APIKeyRef field, resolves the referenced credential name from store, and
// injects the plaintext value into the process environment under that name.
//
// If store is locked or a referenced credential is missing, the affected
// provider fails to initialize with a descriptive error. Other providers
// continue. All errors are collected and returned as a slice.
//
// Implements US-11 acceptance criteria (SEC-22).
func InjectFromConfig(cfg *config.Config, store *Store) []error {
	if store.IsLocked() {
		return []error{ErrStoreLocked}
	}

	var errs []error
	injected := map[string]bool{} // avoid re-injecting duplicates

	for i := range cfg.ModelList {
		model := cfg.ModelList[i]
		ref := strings.TrimSpace(model.APIKeyRef)
		if ref == "" {
			continue
		}
		if injected[ref] {
			continue
		}

		value, err := store.Get(ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("provider %q: credential %q: %w", model.ModelName, ref, err))
			continue
		}

		if err := os.Setenv(ref, value); err != nil {
			errs = append(errs, fmt.Errorf("provider %q: set env %q: %w", model.ModelName, ref, err))
			continue
		}
		injected[ref] = true
		slog.Debug("credentials: injected", "ref", ref, "provider", model.ModelName)
	}

	return errs
}

// ResolveRef looks up a single credential reference and returns its plaintext.
// Returns a descriptive error if the store is locked or the ref is not found.
func ResolveRef(store *Store, ref string) (string, error) {
	if store.IsLocked() {
		return "", ErrStoreLocked
	}
	return store.Get(ref)
}
