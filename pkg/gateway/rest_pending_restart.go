//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/gateway/ctxkey"
)

// RestartGatedKeys is the authoritative list of config keys that require a
// process restart to take effect. Hot-reload keys (prompt_injection_level,
// rate_limits.*, ssrf.*, tool_policies.*, default_tool_policy) are
// deliberately excluded — changing them is picked up on the next request
// without a restart.
//
// Exported so tests and future refactors can reference the canonical list
// without duplicating it.
var RestartGatedKeys = []string{
	"sandbox.mode",
	"sandbox.enabled",
	"sandbox.audit_log",
	"sandbox.allowed_paths",
	"sandbox.skill_trust",
	"storage.retention.session_days",
	"storage.retention.disabled",
	"session.dm_scope",
	"gateway.port",
	"gateway.users",
}

// pendingRestartEntry is a single restart-required change: the dotted key
// whose persisted (on-disk) value diverges from the value that was applied
// at boot time.
type pendingRestartEntry struct {
	Key            string `json:"key"`
	PersistedValue any    `json:"persisted_value"`
	AppliedValue   any    `json:"applied_value"`
}

// HandlePendingRestart handles GET /api/v1/config/pending-restart.
//
// Returns a JSON array of restart-required changes: config keys whose
// persisted (disk) value differs from the boot-time applied value. The diff
// is computed over RestartGatedKeys only — hot-reload keys are never included.
//
// A set-then-revert scenario (admin writes X→Y then Y→X before restart)
// correctly produces an empty array, clearing the UI banner without a restart.
//
// Admin-only: non-admin callers receive 403.
func (a *restAPI) HandlePendingRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	role, _ := r.Context().Value(ctxkey.RoleContextKey{}).(config.UserRole)
	if role != config.UserRoleAdmin {
		jsonErr(w, http.StatusForbidden, "admin required")
		return
	}

	raw, err := os.ReadFile(a.configPath())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "failed to read persisted config")
		return
	}

	var persisted map[string]any
	if err := json.Unmarshal(raw, &persisted); err != nil {
		jsonErr(w, http.StatusInternalServerError, "failed to parse persisted config")
		return
	}

	var applied map[string]any
	if a.appliedConfig != nil {
		appliedRaw, marshalErr := json.Marshal(a.appliedConfig)
		if marshalErr != nil {
			jsonErr(w, http.StatusInternalServerError, "failed to serialize applied config")
			return
		}
		if err := json.Unmarshal(appliedRaw, &applied); err != nil {
			jsonErr(w, http.StatusInternalServerError, "failed to parse applied config")
			return
		}
	}

	diffs := make([]pendingRestartEntry, 0)
	for _, key := range RestartGatedKeys {
		pv := getAtPath(persisted, key)
		av := getAtPath(applied, key)
		if !reflect.DeepEqual(pv, av) {
			diffs = append(diffs, pendingRestartEntry{
				Key:            key,
				PersistedValue: pv,
				AppliedValue:   av,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(diffs); err != nil {
		// Header already written; log only.
		_ = err
	}
}

// deepCopyConfig returns a deep copy of cfg via JSON round-trip. It is called
// exactly once at boot to produce the appliedConfig snapshot; the original cfg
// may be mutated by hot-reload afterward without affecting the snapshot.
// Returns nil when cfg is nil.
func deepCopyConfig(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	var copy config.Config
	if err := json.Unmarshal(raw, &copy); err != nil {
		return nil
	}
	return &copy
}

// getAtPath extracts a value from a nested map[string]any using a dotted path
// such as "sandbox.mode" or "gateway.port". Returns nil when any path segment
// is missing or a non-map intermediate value is encountered.
func getAtPath(m map[string]any, dotted string) any {
	segments := strings.SplitN(dotted, ".", 2)
	if len(segments) == 0 {
		return nil
	}
	val, ok := m[segments[0]]
	if !ok {
		return nil
	}
	if len(segments) == 1 {
		return val
	}
	nested, ok := val.(map[string]any)
	if !ok {
		return nil
	}
	return getAtPath(nested, segments[1])
}
