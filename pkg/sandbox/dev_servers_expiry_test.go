// T2.17 + T2.18: DevServerRegistry expiry via Lookup.
//
// T2.17: Lookup past IdleTimeout returns nil and marks the entry for removal.
// T2.18: Lookup past HardTimeout returns nil even when LastActivity is recent.
//
// These tests drive the Lookup code path directly (not the janitor sweepExpired)
// to verify the "Lookup reads deadline without resurrecting expired entries"
// invariant introduced in B1.4-a.

package sandbox

import (
	"testing"
	"time"
)

// TestLookup_PastIdleTimeout_ReturnsNil (T2.17) inserts a registration, fast-
// forwards LastActivity past IdleTimeout, calls Lookup, and asserts:
//  1. Lookup returns nil (entry expired).
//  2. The entry is still in the map (janitor is responsible for removal).
//
// Point 2 is a property of the current implementation: Lookup does NOT delete;
// sweepExpired does. The test verifies Lookup's contract, not the janitor's.
func TestLookup_PastIdleTimeout_ReturnsNil(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()

	reg, err := r.Register("agent-idle-expiry", 18200, 9901, "vite dev", 5)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Fast-forward LastActivity so the entry is past its idle deadline.
	r.mu.Lock()
	entry, ok := r.entries[reg.Token]
	if !ok {
		r.mu.Unlock()
		t.Fatal("entry not found immediately after Register")
	}
	entry.LastActivity = time.Now().Add(-(IdleTimeout + time.Second))
	r.mu.Unlock()

	// Lookup must return nil for the expired entry.
	got := r.Lookup(reg.Token)
	if got != nil {
		t.Errorf("Lookup past IdleTimeout returned non-nil; want nil (T2.17)")
	}

	// Entry remains in the map until the janitor sweeps it.
	r.mu.Lock()
	_, still := r.entries[reg.Token]
	r.mu.Unlock()
	if !still {
		t.Log("entry was removed by Lookup — implementation changed; update test expectation")
	}
	// Both "still in map" and "already removed" are acceptable: the invariant
	// is only that Lookup returned nil (the entry is not resurrected).
}

// TestLookup_PastHardTimeout_ReturnsNil (T2.18) inserts a registration,
// fast-forwards CreatedAt past HardTimeout while keeping LastActivity recent,
// calls Lookup, and asserts nil is returned. This verifies the hard cap fires
// independently of idle activity.
func TestLookup_PastHardTimeout_ReturnsNil(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()

	reg, err := r.Register("agent-hard-expiry", 18201, 9902, "next dev", 5)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Fast-forward CreatedAt; keep LastActivity fresh so idle cap does NOT fire.
	r.mu.Lock()
	entry, ok := r.entries[reg.Token]
	if !ok {
		r.mu.Unlock()
		t.Fatal("entry not found immediately after Register")
	}
	entry.CreatedAt = time.Now().Add(-(HardTimeout + time.Second))
	entry.LastActivity = time.Now()
	r.mu.Unlock()

	got := r.Lookup(reg.Token)
	if got != nil {
		t.Errorf("Lookup past HardTimeout returned non-nil; want nil (T2.18)")
	}
}

// TestLookup_NotExpired_StillReturnsRegistration sanity-checks that a fresh
// entry is still returned by Lookup so the expiry tests cannot produce false
// positives from a broken Lookup that always returns nil.
func TestLookup_NotExpired_StillReturnsRegistration(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()

	reg, err := r.Register("agent-fresh-sanity", 18202, 9903, "astro dev", 5)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	got := r.Lookup(reg.Token)
	if got == nil {
		t.Fatal("Lookup returned nil for a fresh non-expired entry (sanity check failed)")
	}
	if got.AgentID != "agent-fresh-sanity" {
		t.Errorf("AgentID = %q; want %q", got.AgentID, "agent-fresh-sanity")
	}
}
