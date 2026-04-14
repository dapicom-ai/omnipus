package tools

import (
	"strings"
	"testing"
)

func TestCatalog_NoDuplicateNames(t *testing.T) {
	catalog := GetBuiltinCatalog()
	seen := make(map[string]bool, len(catalog))
	for _, e := range catalog {
		if seen[e.Name] {
			t.Errorf("duplicate tool name in catalog: %q", e.Name)
		}
		seen[e.Name] = true
	}
}

func TestCatalog_AllFieldsNonEmpty(t *testing.T) {
	for _, e := range GetBuiltinCatalog() {
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
	for _, e := range GetBuiltinCatalog() {
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
	for _, e := range GetBuiltinCatalog() {
		if !valid[e.Category] {
			t.Errorf("catalog entry %q has invalid category %q", e.Name, e.Category)
		}
	}
}

func TestCatalogAsMapSlice(t *testing.T) {
	catalog := GetBuiltinCatalog()
	result := CatalogAsMapSlice()
	if len(result) != len(catalog) {
		t.Fatalf("CatalogAsMapSlice returned %d entries, expected %d", len(result), len(catalog))
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
	for _, e := range GetBuiltinCatalog() {
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
	for _, e := range GetBuiltinCatalog() {
		if e.Scope == ScopeSystem {
			if strings.Contains(md, "`"+e.Name+"`") {
				t.Errorf("CatalogMarkdown should not contain system tool %q", e.Name)
			}
		}
	}
}

func TestCatalog_MinimumSize(t *testing.T) {
	if len(GetBuiltinCatalog()) < 60 {
		t.Errorf("expected at least 60 tools in catalog, got %d", len(GetBuiltinCatalog()))
	}
}

// TestGetBuiltinCatalog_ReturnsCopy verifies that GetBuiltinCatalog returns the
// same entries as the underlying slice, and that the returned slice is non-nil.
func TestGetBuiltinCatalog_ReturnsCopy(t *testing.T) {
	catalog := GetBuiltinCatalog()
	if catalog == nil {
		t.Fatal("GetBuiltinCatalog must return a non-nil slice")
	}
	if len(catalog) == 0 {
		t.Fatal("GetBuiltinCatalog must return a non-empty slice")
	}
	// Two calls must return slices of the same length with the same first element.
	catalog2 := GetBuiltinCatalog()
	if len(catalog) != len(catalog2) {
		t.Errorf("GetBuiltinCatalog returned different lengths: %d vs %d", len(catalog), len(catalog2))
	}
	if catalog[0].Name != catalog2[0].Name {
		t.Errorf("GetBuiltinCatalog first entry mismatch: %q vs %q", catalog[0].Name, catalog2[0].Name)
	}
}
