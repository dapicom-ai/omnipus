// Tests for DevServerRegistry: per-agent cap, per-gateway cap, janitor,
// and Lookup-touches-LastActivity behaviour.

package sandbox

import (
	"errors"
	"testing"
	"time"
)

// TestDevServerRegistry_PerAgentCap exercises the single-active-server
// rule. A second registration for the same agent gets
// ErrPerAgentCap.
func TestDevServerRegistry_PerAgentCap(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()

	if _, err := r.Register("agent1", 18000, 1, "next dev", 10); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err := r.Register("agent1", 18001, 2, "next dev", 10)
	if !errors.Is(err, ErrPerAgentCap) {
		t.Errorf("second Register err = %v; want ErrPerAgentCap", err)
	}
}

// TestDevServerRegistry_GatewayCap exercises the gateway-wide cap and
// confirms the error carries the EarliestExpiry hint mandates.
func TestDevServerRegistry_GatewayCap(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()

	if _, err := r.Register("a", 18000, 1, "next dev", 1); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err := r.Register("b", 18001, 2, "next dev", 1)
	var capErr ErrGatewayCap
	if !errors.As(err, &capErr) {
		t.Fatalf("expected ErrGatewayCap, got %v", err)
	}
	if capErr.Current != 1 || capErr.Max != 1 {
		t.Errorf("ErrGatewayCap = %+v; want Current=1 Max=1", capErr)
	}
	if capErr.EarliestExpiry.IsZero() {
		t.Error("EarliestExpiry is zero; want a real timestamp")
	}
	if !errMessageContains(capErr, "too many concurrent dev servers") {
		t.Errorf("error message %q does not match MAJ-005 wording", capErr.Error())
	}
}

// TestDevServerRegistry_LookupTouchesActivity verifies that a successful
// Lookup slides the idle timer forward — the proxy uses this to keep an
// active session alive.
func TestDevServerRegistry_LookupTouchesActivity(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()
	reg, err := r.Register("a", 18000, 1, "next dev", 5)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	original := reg.LastActivity

	// Sleep enough to make the difference observable. 5 ms is plenty.
	time.Sleep(5 * time.Millisecond)
	got := r.Lookup(reg.Token)
	if got == nil {
		t.Fatal("Lookup returned nil for valid token")
	}
	if !got.LastActivity.After(original) {
		t.Errorf("LastActivity not advanced; before=%v after=%v", original, got.LastActivity)
	}
}

// TestDevServerRegistry_Unregister removes the entry and is idempotent.
func TestDevServerRegistry_Unregister(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()
	reg, _ := r.Register("a", 18000, 1, "next dev", 5)
	if !r.Unregister(reg.Token) {
		t.Error("Unregister returned false for existing token")
	}
	if r.Unregister(reg.Token) {
		t.Error("Unregister returned true for already-removed token")
	}
	if r.Count() != 0 {
		t.Errorf("Count = %d; want 0", r.Count())
	}
}

// TestDevServerRegistry_UnregisterByAgent removes the entry owned by the
// supplied agent. Used when an agent is deleted.
func TestDevServerRegistry_UnregisterByAgent(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()
	if _, err := r.Register("agent1", 18000, 1, "next dev", 5); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !r.UnregisterByAgent("agent1") {
		t.Error("UnregisterByAgent returned false")
	}
	if r.Count() != 0 {
		t.Errorf("Count = %d; want 0", r.Count())
	}
}

// TestDevServerRegistry_JanitorExpiresIdle exercises the janitor by
// fast-forwarding LastActivity. Since the janitor runs every 30 s in
// production we drive sweepExpired directly to avoid a slow test.
func TestDevServerRegistry_JanitorExpiresIdle(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()
	reg, _ := r.Register("a", 18000, 1, "next dev", 5)
	r.mu.Lock()
	r.entries[reg.Token].LastActivity = time.Now().Add(-IdleTimeout - time.Second)
	r.mu.Unlock()

	r.sweepExpired()
	if r.Count() != 0 {
		t.Errorf("Count after sweep = %d; want 0 (idle expiry should have removed entry)", r.Count())
	}
}

// TestDevServerRegistry_JanitorExpiresHardCap exercises the hard cap.
// Same approach: drive CreatedAt back past HardTimeout.
func TestDevServerRegistry_JanitorExpiresHardCap(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()
	reg, _ := r.Register("a", 18000, 1, "next dev", 5)
	r.mu.Lock()
	r.entries[reg.Token].CreatedAt = time.Now().Add(-HardTimeout - time.Second)
	// Keep LastActivity recent so we know the hard cap is what fires.
	r.entries[reg.Token].LastActivity = time.Now()
	r.mu.Unlock()

	r.sweepExpired()
	if r.Count() != 0 {
		t.Errorf("Count after sweep = %d; want 0 (hard cap should have removed entry)", r.Count())
	}
}

// TestDevServerRegistry_TokenIsRandom asserts each registration produces
// a distinct token (collision would let one agent's request leak into
// another's URL).
func TestDevServerRegistry_TokenIsRandom(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()
	tokens := make(map[string]bool)
	for i := 0; i < 20; i++ {
		reg, err := r.Register(fakeID(i), int32(18000+i), 1000+i, "next dev", 100)
		if err != nil {
			t.Fatalf("Register %d: %v", i, err)
		}
		if tokens[reg.Token] {
			t.Errorf("token collision at iter %d: %q", i, reg.Token)
		}
		tokens[reg.Token] = true
	}
}

func fakeID(i int) string {
	return "agent-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
}

func errMessageContains(err error, substr string) bool {
	return err != nil && stringContains(err.Error(), substr)
}

func stringContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
