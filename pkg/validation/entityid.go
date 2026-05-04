// Package validation provides cross-package input validators. Extracted from
// pkg/gateway to make identifier checks reachable from pkg/agent (memory-retro
// path validation) without a gateway→agent cycle. Spec v7 FR-062, MAJ-002.
package validation

import (
	"fmt"
	"strings"
)

// EntityID rejects IDs that contain path separators, "..", or NUL bytes, and
// also rejects the empty string. The exact behavior must match the previous
// unexported gateway.validateEntityID — the BDD contract is "same inputs,
// same errors" so callers that already trusted this function continue to do
// so without drift.
func EntityID(id string) error {
	if id == "" {
		return fmt.Errorf("id must not be empty")
	}
	if strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") || strings.ContainsRune(id, 0) {
		return fmt.Errorf("invalid id")
	}
	return nil
}
