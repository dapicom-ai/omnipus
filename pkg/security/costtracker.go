// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package security

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// CostTrackerState is the persisted state for the global daily cost cap (FR-026).
type CostTrackerState struct {
	Date    string  `json:"date"`     // "YYYY-MM-DD" UTC
	CostUSD float64 `json:"cost_usd"` // accumulated cost for that day
}

// CostTracker persists daily cost accumulator to disk so it survives restarts.
type CostTracker struct {
	filePath string
}

// NewCostTracker creates a cost tracker that reads/writes the given JSON file.
// The directory is created if it doesn't exist.
func NewCostTracker(filePath string) *CostTracker {
	return &CostTracker{filePath: filePath}
}

// Load reads the persisted cost state. Returns zero state if file doesn't exist
// or is for a different day.
func (ct *CostTracker) Load() CostTrackerState {
	data, err := os.ReadFile(ct.filePath)
	if err != nil {
		return CostTrackerState{}
	}
	var state CostTrackerState
	if err := json.Unmarshal(data, &state); err != nil {
		slog.Warn("costtracker: invalid state file, resetting", "path", ct.filePath, "error", err)
		return CostTrackerState{}
	}
	today := time.Now().UTC().Format("2006-01-02")
	if state.Date != today {
		return CostTrackerState{}
	}
	return state
}

// Save persists the current cost state atomically (temp file + rename).
func (ct *CostTracker) Save(state CostTrackerState) error {
	if err := os.MkdirAll(filepath.Dir(ct.filePath), 0o700); err != nil {
		return fmt.Errorf("costtracker: mkdir: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("costtracker: marshal: %w", err)
	}
	tmp := ct.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("costtracker: write temp: %w", err)
	}
	if err := os.Rename(tmp, ct.filePath); err != nil {
		return fmt.Errorf("costtracker: rename: %w", err)
	}
	return nil
}

// LoadIntoRegistry loads persisted cost state into a RateLimiterRegistry.
func (ct *CostTracker) LoadIntoRegistry(r *RateLimiterRegistry) {
	state := ct.Load()
	if state.Date != "" && state.CostUSD > 0 {
		r.LoadDailyCost(state.CostUSD, state.Date)
		slog.Info("costtracker: restored daily cost", "date", state.Date, "cost_usd", state.CostUSD)
	}
}

// SaveFromRegistry persists the current registry cost state to disk.
func (ct *CostTracker) SaveFromRegistry(r *RateLimiterRegistry) error {
	today := time.Now().UTC().Format("2006-01-02")
	cost := r.GetDailyCost()
	return ct.Save(CostTrackerState{Date: today, CostUSD: cost})
}
