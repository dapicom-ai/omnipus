package tools

import (
	"strings"
	"testing"
)

func TestCatalog_NoDuplicateNames(t *testing.T) {
	seen := make(map[string]bool, len(BuiltinCatalog))
	for _, e := range BuiltinCatalog {
		if seen[e.Name] {
			t.Errorf("duplicate tool name in catalog: %q", e.Name)
		}
		seen[e.Name] = true
	}
}

func TestCatalog_AllFieldsNonEmpty(t *testing.T) {
	for _, e := range BuiltinCatalog {
		if e.Name == "" {
			t.Error("catalog entry has empty Name")
		}
		if e.Description == "" {
			t.Errorf("catalog entry %q has empty Description", e.Name)
		}
		if e.Scope == "" {
			t.Errorf("catalog entry %q has empty Scope", e.Name)
		}
		if e.Category == "" {
			t.Errorf("catalog entry %q has empty Category", e.Name)
		}
	}
}

func TestCatalog_ValidScopes(t *testing.T) {
	valid := map[ToolScope]bool{ScopeSystem: true, ScopeCore: true, ScopeGeneral: true}
	for _, e := range BuiltinCatalog {
		if !valid[e.Scope] {
			t.Errorf("catalog entry %q has invalid scope %q", e.Name, e.Scope)
		}
	}
}

func TestCatalog_ValidCategories(t *testing.T) {
	valid := map[ToolCategory]bool{
		CategoryFile: true, CategoryCode: true, CategoryWeb: true,
		CategoryBrowser: true, CategoryCommunication: true, CategoryTask: true,
		CategoryAutomation: true, CategorySearch: true, CategorySkills: true,
		CategoryHardware: true, CategorySystem: true,
	}
	for _, e := range BuiltinCatalog {
		if !valid[e.Category] {
			t.Errorf("catalog entry %q has invalid category %q", e.Name, e.Category)
		}
	}
}

func TestCatalogAsMapSlice(t *testing.T) {
	result := CatalogAsMapSlice()
	if len(result) != len(BuiltinCatalog) {
		t.Fatalf("CatalogAsMapSlice returned %d entries, expected %d", len(result), len(BuiltinCatalog))
	}
	for i, m := range result {
		for _, key := range []string{"name", "description", "scope", "category"} {
			if _, ok := m[key]; !ok {
				t.Errorf("entry %d missing key %q", i, key)
			}
		}
	}
}

func TestCatalogMarkdown_ContainsAllNonSystemTools(t *testing.T) {
	md := CatalogMarkdown()
	for _, e := range BuiltinCatalog {
		if e.Scope == ScopeSystem {
			continue
		}
		if !strings.Contains(md, e.Name) {
			t.Errorf("CatalogMarkdown missing tool %q", e.Name)
		}
	}
}

func TestCatalogMarkdown_ExcludesSystemTools(t *testing.T) {
	md := CatalogMarkdown()
	for _, e := range BuiltinCatalog {
		if e.Scope == ScopeSystem {
			if strings.Contains(md, "`"+e.Name+"`") {
				t.Errorf("CatalogMarkdown should not contain system tool %q", e.Name)
			}
		}
	}
}

func TestCatalog_MinimumSize(t *testing.T) {
	if len(BuiltinCatalog) < 60 {
		t.Errorf("expected at least 60 tools in catalog, got %d", len(BuiltinCatalog))
	}
}
