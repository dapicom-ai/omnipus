// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Package security provides network security controls for Omnipus.
//
// This file implements US-13 rate limiting (per-agent, per-channel, global cost cap)
// from the Wave 2 security spec.
package security

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// systemAgentID is exempt from all rate limits per US-13 (FR-025).
const systemAgentID = "omnipus-system"

// RateLimitScope identifies the scope of a rate limit.
type RateLimitScope string

const (
	// ScopeAgent is a per-agent sliding window limit.
	ScopeAgent RateLimitScope = "agent"
	// ScopeChannel is a per-channel outbound message limit.
	ScopeChannel RateLimitScope = "channel"
	// ScopeGlobal is the global daily cost cap.
	ScopeGlobal RateLimitScope = "global"
)

// RateLimitResult is returned by all rate limit checks.
type RateLimitResult struct {
	Allowed           bool
	RetryAfterSeconds float64 // > 0 when Allowed is false
	PolicyRule        string  // explains which limit was hit
}

// SlidingWindow is a thread-safe sliding window counter for rate limiting.
// It tracks a (scope, scopeID, resource) triple and enforces a maximum number
// of events within a rolling time window.
type SlidingWindow struct {
	mu         sync.Mutex
	events     []time.Time
	maxCount   int
	windowSize time.Duration
	scope      RateLimitScope
	scopeID    string
	resource   string
}

// NewSlidingWindow creates a SlidingWindow that allows at most limit events
// within window. scope, scopeID, and resource are used to construct policy rule
// messages in denied results.
func NewSlidingWindow(limit int, window time.Duration, scope RateLimitScope, scopeID, resource string) *SlidingWindow {
	return &SlidingWindow{
		events:     make([]time.Time, 0, limit+1),
		maxCount:   limit,
		windowSize: window,
		scope:      scope,
		scopeID:    scopeID,
		resource:   resource,
	}
}

// Allow records an event and reports whether it is within the limit.
// If denied, RetryAfterSeconds indicates when the next slot opens.
func (sw *SlidingWindow) Allow() RateLimitResult {
	return sw.allowAt(time.Now())
}

// allowAt is the internal implementation, accepting a clock value for testability.
func (sw *SlidingWindow) allowAt(now time.Time) RateLimitResult {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	cutoff := now.Add(-sw.windowSize)
	// Prune expired events from the front
	i := 0
	for i < len(sw.events) && sw.events[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		sw.events = sw.events[i:]
	}

	if len(sw.events) < sw.maxCount {
		sw.events = append(sw.events, now)
		return RateLimitResult{Allowed: true}
	}

	// Window is full — compute when the oldest event expires
	oldest := sw.events[0]
	retryAt := oldest.Add(sw.windowSize)
	retryAfter := retryAt.Sub(now).Seconds()
	if retryAfter < 0.001 {
		retryAfter = 0.001
	}

	rule := fmt.Sprintf("rate_limit: %s limit %d/%s exceeded for %s %q",
		sw.resource, sw.maxCount, sw.windowSize, sw.scope, sw.scopeID)

	slog.Info("ratelimit: sliding window rejected",
		"scope", sw.scope,
		"scope_id", sw.scopeID,
		"resource", sw.resource,
		"retry_after_seconds", retryAfter,
	)

	return RateLimitResult{
		Allowed:           false,
		RetryAfterSeconds: retryAfter,
		PolicyRule:        rule,
	}
}

// --------------------------------------------------------------------------
// RateLimiterRegistry — manages named SlidingWindows and global cost cap
// --------------------------------------------------------------------------

// RateLimiterRegistry manages per-agent, per-channel sliding windows and the
// global daily cost cap. All counters are in-memory and reset on restart;
// the cost accumulator additionally resets at UTC midnight.
type RateLimiterRegistry struct {
	mu      sync.RWMutex
	windows map[string]*SlidingWindow

	// Global daily cost accumulator
	costMu       sync.Mutex
	dailyCostUSD float64
	dailyCostCap float64
	costDay      string // "YYYY-MM-DD" UTC
}

// NewRateLimiterRegistry creates an empty registry.
func NewRateLimiterRegistry() *RateLimiterRegistry {
	return &RateLimiterRegistry{
		windows: make(map[string]*SlidingWindow),
	}
}

// SetDailyCostCap sets the maximum USD spend per UTC day.
// A cap of 0 or negative means no cap is applied.
func (r *RateLimiterRegistry) SetDailyCostCap(capUSD float64) {
	r.costMu.Lock()
	r.dailyCostCap = capUSD
	r.costMu.Unlock()
}

// GetOrCreate returns the SlidingWindow for the given key, creating it with
// the supplied limit and window duration if it does not yet exist.
func (r *RateLimiterRegistry) GetOrCreate(key string, limit int, window time.Duration, scope RateLimitScope, scopeID, resource string) *SlidingWindow {
	r.mu.RLock()
	sw, ok := r.windows[key]
	r.mu.RUnlock()
	if ok {
		return sw
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if sw, ok = r.windows[key]; ok {
		return sw
	}
	sw = NewSlidingWindow(limit, window, scope, scopeID, resource)
	r.windows[key] = sw
	return sw
}

// CheckGlobalCostCap checks and records costUSD against the global daily cost cap.
//
// The system agent (omnipus-system) is always exempt.
// When no cap is configured (cap <= 0), all calls are allowed.
// When the accumulated cost + costUSD would exceed the cap, the call is denied.
func (r *RateLimiterRegistry) CheckGlobalCostCap(costUSD float64, agentID string) RateLimitResult {
	if agentID == systemAgentID {
		return RateLimitResult{Allowed: true, PolicyRule: "system agent exempt from cost cap"}
	}

	r.costMu.Lock()
	defer r.costMu.Unlock()

	if r.dailyCostCap <= 0 {
		return RateLimitResult{Allowed: true}
	}

	today := time.Now().UTC().Format("2006-01-02")
	if r.costDay != today {
		r.costDay = today
		r.dailyCostUSD = 0
	}

	if r.dailyCostUSD+costUSD > r.dailyCostCap {
		rule := fmt.Sprintf("global daily cost cap exceeded ($%.2f)", r.dailyCostCap)

		// RetryAfterSeconds is time until UTC midnight when the daily counter resets.
		now := time.Now().UTC()
		midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		retryAfter := midnight.Sub(now).Seconds()

		slog.Warn("ratelimit: global cost cap exceeded",
			"agent_id", agentID,
			"daily_cost_usd", r.dailyCostUSD,
			"requested_usd", costUSD,
			"cap_usd", r.dailyCostCap,
			"retry_after_seconds", retryAfter,
		)
		return RateLimitResult{
			Allowed:           false,
			RetryAfterSeconds: retryAfter,
			PolicyRule:        rule,
		}
	}

	r.dailyCostUSD += costUSD
	return RateLimitResult{Allowed: true}
}

// GetDailyCost returns the accumulated cost for the current UTC day.
func (r *RateLimiterRegistry) GetDailyCost() float64 {
	today := time.Now().UTC().Format("2006-01-02")
	r.costMu.Lock()
	defer r.costMu.Unlock()
	if r.costDay != today {
		return 0
	}
	return r.dailyCostUSD
}

// LoadDailyCost sets the accumulated cost for a specific date (for testing or
// restore-from-disk scenarios). date must be "YYYY-MM-DD" UTC.
func (r *RateLimiterRegistry) LoadDailyCost(costUSD float64, date string) {
	r.costMu.Lock()
	r.costDay = date
	r.dailyCostUSD = costUSD
	r.costMu.Unlock()
}

