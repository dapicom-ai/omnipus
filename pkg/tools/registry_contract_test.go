// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCatalog_AllToolsHaveCategory verifies that every entry in the builtin catalog
// has a non-empty Category. This is the FR-059 / FR-067 BaseTool mixin contract.
//
// BDD: Given the builtin tool catalog,
//
//	When Category() is called on each entry,
//	Then no entry returns an empty ToolCategory.
//
// Traces to: pkg/tools/catalog.go — Tool interface Category() (FR-059, FR-067).
func TestCatalog_AllToolsHaveCategory(t *testing.T) {
	for _, e := range GetBuiltinCatalog() {
		assert.NotEmpty(t, string(e.Category),
			"catalog entry %q must have a non-empty Category (BaseTool mixin contract)", e.Name)
	}
}

// TestCatalog_AllToolsHaveScope verifies that every entry in the builtin catalog
// has a recognized Scope value (ScopeCore or ScopeGeneral; ScopeSystem is retired FR-045).
//
// BDD: Given the builtin tool catalog,
//
//	When Scope() is checked on each entry,
//	Then every entry has Scope == ScopeCore or ScopeCore == ScopeGeneral (never ScopeSystem or empty).
//
// Traces to: pkg/tools/base.go — ScopeSystem constant retired (FR-045).
func TestCatalog_AllToolsHaveScope(t *testing.T) {
	valid := map[ToolScope]bool{ScopeCore: true, ScopeGeneral: true}
	for _, e := range GetBuiltinCatalog() {
		if !valid[e.Scope] {
			t.Errorf("catalog entry %q has invalid scope %q (FR-045: ScopeSystem is retired)", e.Name, e.Scope)
		}
	}
}

// TestToolsToProviderDefs_PreservesNamesAndDescriptions verifies that
// ToolsToProviderDefs converts a slice of tools to provider definitions preserving
// each tool's name (sanitized) and description.
//
// BDD: Given two tools with distinct names and descriptions,
//
//	When ToolsToProviderDefs is called,
//	Then the result has two entries with sanitized names and matching descriptions.
//
// Traces to: pkg/tools/registry.go — ToolsToProviderDefs (FR-003, FR-041).
func TestToolsToProviderDefs_PreservesNamesAndDescriptions(t *testing.T) {
	tools := []Tool{
		&mockRegistryTool{name: "my.tool", desc: "first tool", params: map[string]any{"type": "object"}, result: SilentResult("ok")},
		&mockRegistryTool{name: "other_tool", desc: "second tool", params: map[string]any{"type": "object"}, result: SilentResult("ok")},
	}

	defs := ToolsToProviderDefs(tools)

	if len(defs) != 2 {
		t.Fatalf("expected 2 provider defs, got %d", len(defs))
	}

	// Names are sanitized (dots → underscores).
	assert.Equal(t, "my_tool", defs[0].Function.Name, "dot in name must be sanitized to underscore")
	assert.Equal(t, "other_tool", defs[1].Function.Name)

	assert.Equal(t, "first tool", defs[0].Function.Description)
	assert.Equal(t, "second tool", defs[1].Function.Description)
}

// TestToolsToProviderDefs_EmptySlice verifies that an empty input slice returns
// a non-nil empty result (not nil) so callers can range over it safely.
//
// BDD: Given an empty []Tool,
//
//	When ToolsToProviderDefs is called,
//	Then the result is a non-nil empty slice.
//
// Traces to: pkg/tools/registry.go — ToolsToProviderDefs nil-safety.
func TestToolsToProviderDefs_EmptySlice(t *testing.T) {
	defs := ToolsToProviderDefs(nil)
	assert.NotNil(t, defs, "ToolsToProviderDefs(nil) must return a non-nil slice")
	assert.Empty(t, defs)

	defs2 := ToolsToProviderDefs([]Tool{})
	assert.NotNil(t, defs2)
	assert.Empty(t, defs2)
}
