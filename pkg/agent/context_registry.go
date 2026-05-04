package agent

import "sync"

// ContextBuilderRegistry is a thin, concurrency-safe registry that maps an
// agent ID to its ContextBuilder. It is the broadcast channel for config-change
// invalidation (FR-061): when an operator writes a sandbox or dev-mode-bypass
// setting, the REST handler calls InvalidateAllContextBuilders so every agent's
// next turn picks up the new preamble without a restart.
//
// Stale entries (agents that have been deleted but whose Unregister was never
// called) are harmless: InvalidateCache on a no-longer-used ContextBuilder is a
// cheap no-op.
type ContextBuilderRegistry struct {
	mu sync.Map // agentID (string) → *ContextBuilder
}

// NewContextBuilderRegistry returns an empty, ready-to-use registry.
func NewContextBuilderRegistry() *ContextBuilderRegistry {
	return &ContextBuilderRegistry{}
}

// Register associates agentID with cb. Replaces any existing entry for the
// same ID (e.g. after a hot-reload creates a new ContextBuilder for an existing
// agent).
func (r *ContextBuilderRegistry) Register(agentID string, cb *ContextBuilder) {
	r.mu.Store(agentID, cb)
}

// Unregister removes the entry for agentID. Safe to call when the entry does
// not exist.
func (r *ContextBuilderRegistry) Unregister(agentID string) {
	r.mu.Delete(agentID)
}

// InvalidateAllContextBuilders iterates every registered ContextBuilder and
// calls InvalidateCache on each. This forces a full rebuild of the system
// prompt (including the env preamble) on the next turn. The iteration is
// lock-free (sync.Map.Range) and safe for concurrent reads and writes.
func (r *ContextBuilderRegistry) InvalidateAllContextBuilders() {
	r.mu.Range(func(_, value any) bool {
		if cb, ok := value.(*ContextBuilder); ok && cb != nil {
			cb.InvalidateCache()
		}
		return true // continue iteration
	})
}
