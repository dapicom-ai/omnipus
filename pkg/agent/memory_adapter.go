// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

package agent

import (
	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// MemoryStoreAdapter wraps *MemoryStore to implement tools.MemoryAccess.
// The adapter converts between the agent-side types (LongTermEntry, Retro)
// and the tools-side mirror types (tools.MemoryEntry, tools.MemoryRetro),
// keeping pkg/agent and pkg/tools import-cycle-free.
type MemoryStoreAdapter struct {
	ms *MemoryStore
}

// NewMemoryStoreAdapter wraps ms in a tools.MemoryAccess implementation.
func NewMemoryStoreAdapter(ms *MemoryStore) *MemoryStoreAdapter {
	return &MemoryStoreAdapter{ms: ms}
}

// AppendLongTerm delegates to MemoryStore.AppendLongTerm (MemoryWriter).
func (a *MemoryStoreAdapter) AppendLongTerm(content, category string) error {
	return a.ms.AppendLongTerm(content, category)
}

// AppendRetro converts tools.MemoryRetro to agent.Retro and delegates. The
// Trigger field crosses a type boundary here: pkg/tools sees it as a free
// string (mirror-struct to avoid cycles), pkg/agent keeps it typed so a future
// refactor can add exhaustive-switch checks on triggers.
func (a *MemoryStoreAdapter) AppendRetro(sessionID string, r tools.MemoryRetro) error {
	return a.ms.AppendRetro(sessionID, Retro{
		Timestamp:        r.Timestamp,
		Trigger:          RecapTrigger(r.Trigger),
		Fallback:         r.Fallback,
		FallbackReason:   r.FallbackReason,
		Recap:            r.Recap,
		WentWell:         r.WentWell,
		NeedsImprovement: r.NeedsImprovement,
	})
}

// SearchEntries delegates to MemoryStore.SearchEntries and converts results.
// Category crosses the same type boundary as Trigger in AppendRetro.
func (a *MemoryStoreAdapter) SearchEntries(query string, limit int) ([]tools.MemoryEntry, error) {
	agentEntries, err := a.ms.SearchEntries(query, limit)
	if err != nil {
		return nil, err
	}
	result := make([]tools.MemoryEntry, len(agentEntries))
	for i, e := range agentEntries {
		result[i] = tools.MemoryEntry{
			Timestamp: e.Timestamp,
			Category:  string(e.Category),
			Content:   e.Content,
		}
	}
	return result, nil
}
