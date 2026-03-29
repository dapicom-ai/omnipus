// Omnipus — System Agent Tools
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Package systools implements the 35 exclusive system.* tools for the
// Omnipus system agent per BRD Appendix D §D.4.
package systools

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/credentials"
	"github.com/dapicom-ai/omnipus/pkg/fileutil"
)

// Deps bundles all shared dependencies for system tools.
type Deps struct {
	// Home is the ~/.omnipus/ data directory path.
	Home string
	// ConfigPath is the path to config.json.
	ConfigPath string
	// Cfg is the in-memory config (pointer, mutated in place by config tools).
	Cfg *config.Config
	// SaveConfig persists Cfg to ConfigPath. Must be called with single-writer
	// serialization by the caller (e.g., a channel or mutex in the gateway).
	SaveConfig func() error
	// CredStore is the encrypted credential store.
	CredStore *credentials.Store
}

// readEntity reads a per-entity JSON file from dir/<id>.json.
func readEntity(dir, id string, v any) error {
	path := entityPath(dir, id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("NOT_FOUND: %s", id)
		}
		return fmt.Errorf("read entity %s: %w", id, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse entity %s: %w", id, err)
	}
	return nil
}

// writeEntity atomically writes a per-entity JSON file to dir/<id>.json.
func writeEntity(dir, id string, v any) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal entity %s: %w", id, err)
	}
	return fileutil.WriteFileAtomic(entityPath(dir, id), data, 0o600)
}

// deleteEntity removes dir/<id>.json.
func deleteEntity(dir, id string) error {
	path := entityPath(dir, id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete entity %s: %w", id, err)
	}
	return nil
}

// listEntities reads all JSON files in dir and unmarshals them into a slice of T.
func listEntities[T any](dir string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	var result []T
	for _, e := range entries {
		if e.IsDir() || len(e.Name()) < 6 || e.Name()[len(e.Name())-5:] != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		data, err := os.ReadFile(entityPath(dir, id))
		if err != nil {
			continue
		}
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			continue
		}
		result = append(result, v)
	}
	return result, nil
}

func entityPath(dir, id string) string {
	return dir + "/" + id + ".json"
}

// nowISO returns the current UTC time as an ISO 8601 string.
func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// successJSON marshals v and returns it as a string for ForLLM.
func successJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return `{"success":true}`
	}
	return string(b)
}

// errorJSON returns a consistent error response per D.10.1.
func errorJSON(code, message, suggestion string) string {
	b, _ := json.MarshalIndent(map[string]any{
		"success": false,
		"error": map[string]any{
			"code":       code,
			"message":    message,
			"suggestion": suggestion,
		},
	}, "", "  ")
	return string(b)
}
