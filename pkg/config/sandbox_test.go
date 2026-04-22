// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package config

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestOmnipusSandboxConfig_ResolvedMode_Precedence verifies the Sprint-J
// legacy mapping between the new Mode field and the deprecated Enabled
// bool. Mode (when set) always wins; Enabled is a fallback.
func TestOmnipusSandboxConfig_ResolvedMode_Precedence(t *testing.T) {
	cases := []struct {
		name string
		cfg  OmnipusSandboxConfig
		want string
	}{
		{"explicit enforce", OmnipusSandboxConfig{Mode: "enforce"}, "enforce"},
		{"explicit permissive", OmnipusSandboxConfig{Mode: "permissive"}, "permissive"},
		{"explicit off", OmnipusSandboxConfig{Mode: "off"}, "off"},
		{"mode wins over enabled=true", OmnipusSandboxConfig{Mode: "off", Enabled: true}, "off"},
		{"mode wins over enabled=false", OmnipusSandboxConfig{Mode: "enforce", Enabled: false}, "enforce"},
		{"legacy enabled=true", OmnipusSandboxConfig{Enabled: true}, "enforce"},
		{"legacy enabled=false", OmnipusSandboxConfig{Enabled: false}, "off"},
		{"zero value", OmnipusSandboxConfig{}, "off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.ResolvedMode()
			if got != tc.want {
				t.Errorf("ResolvedMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSSRFConfig_AllowInternalRemainsStringList pins the invariant that
// OmnipusSSRFConfig.AllowInternal must stay []string (heterogeneous:
// hostnames, IPs, CIDRs all in one list). Any refactor that introduces
// a bool or a separate allow_internal_cidrs field flips the answer to
// this test and fails loudly.
func TestSSRFConfig_AllowInternalRemainsStringList(t *testing.T) {
	rt := reflect.TypeOf((*OmnipusSSRFConfig)(nil)).Elem()
	field, ok := rt.FieldByName("AllowInternal")
	if !ok {
		t.Fatalf("OmnipusSSRFConfig.AllowInternal field missing — regression")
	}
	if got := field.Type.String(); got != "[]string" {
		t.Fatalf("OmnipusSSRFConfig.AllowInternal type = %q, want %q — regression",
			got, "[]string")
	}
	// Defensively assert no neighbouring field was introduced that would
	// compete with AllowInternal. "AllowInternalCIDRs" was the shape the
	// spec's earlier draft proposed and the revision rejected; guard
	// against a future re-introduction.
	if _, leaked := rt.FieldByName("AllowInternalCIDRs"); leaked {
		t.Fatalf("OmnipusSSRFConfig.AllowInternalCIDRs must not exist — regression")
	}
}

// --- SkillTrustLevel UnmarshalJSON ---

// TestSkillTrustLevel_UnmarshalJSON_ValidValues confirms all three canonical
// strings round-trip through UnmarshalJSON without error.
func TestSkillTrustLevel_UnmarshalJSON_ValidValues(t *testing.T) {
	cases := []struct {
		raw  string
		want SkillTrustLevel
	}{
		{`"block_unverified"`, SkillTrustBlockUnverified},
		{`"warn_unverified"`, SkillTrustWarnUnverified},
		{`"allow_all"`, SkillTrustAllowAll},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			var got SkillTrustLevel
			if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSkillTrustLevel_UnmarshalJSON_UppercaseRejected confirms that
// uppercased variants ("BLOCK_UNVERIFIED") are rejected at decode time.
func TestSkillTrustLevel_UnmarshalJSON_UppercaseRejected(t *testing.T) {
	var got SkillTrustLevel
	if err := json.Unmarshal([]byte(`"BLOCK_UNVERIFIED"`), &got); err == nil {
		t.Fatal("expected error for BLOCK_UNVERIFIED, got nil")
	}
}

// TestSkillTrustLevel_UnmarshalJSON_UnknownRejected confirms that arbitrary
// unknown strings are rejected at decode time.
func TestSkillTrustLevel_UnmarshalJSON_UnknownRejected(t *testing.T) {
	var got SkillTrustLevel
	if err := json.Unmarshal([]byte(`"ridiculous"`), &got); err == nil {
		t.Fatal("expected error for unknown value, got nil")
	}
}

// TestSkillTrustLevel_UnmarshalJSON_EmptyAccepted confirms that the empty
// string is accepted (omitted field in config.json).
func TestSkillTrustLevel_UnmarshalJSON_EmptyAccepted(t *testing.T) {
	var got SkillTrustLevel
	if err := json.Unmarshal([]byte(`""`), &got); err != nil {
		t.Fatalf("unexpected error for empty string: %v", err)
	}
}

// --- PromptInjectionLevel UnmarshalJSON ---

// TestPromptInjectionLevel_UnmarshalJSON_ValidValues confirms all three
// canonical strings round-trip through UnmarshalJSON without error.
func TestPromptInjectionLevel_UnmarshalJSON_ValidValues(t *testing.T) {
	cases := []struct {
		raw  string
		want PromptInjectionLevel
	}{
		{`"low"`, PromptInjectionLow},
		{`"medium"`, PromptInjectionMedium},
		{`"high"`, PromptInjectionHigh},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			var got PromptInjectionLevel
			if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPromptInjectionLevel_UnmarshalJSON_UppercaseRejected confirms that
// "MEDIUM" is rejected at decode time.
func TestPromptInjectionLevel_UnmarshalJSON_UppercaseRejected(t *testing.T) {
	var got PromptInjectionLevel
	if err := json.Unmarshal([]byte(`"MEDIUM"`), &got); err == nil {
		t.Fatal("expected error for MEDIUM, got nil")
	}
}

// TestPromptInjectionLevel_UnmarshalJSON_UnknownRejected confirms that
// arbitrary unknown strings are rejected at decode time.
func TestPromptInjectionLevel_UnmarshalJSON_UnknownRejected(t *testing.T) {
	var got PromptInjectionLevel
	if err := json.Unmarshal([]byte(`"ridiculous"`), &got); err == nil {
		t.Fatal("expected error for unknown value, got nil")
	}
}

// TestPromptInjectionLevel_UnmarshalJSON_EmptyAccepted confirms that the
// empty string is accepted (config may legitimately omit this field).
func TestPromptInjectionLevel_UnmarshalJSON_EmptyAccepted(t *testing.T) {
	var got PromptInjectionLevel
	if err := json.Unmarshal([]byte(`""`), &got); err != nil {
		t.Fatalf("unexpected error for empty string: %v", err)
	}
}

// TestAllowedPaths_ReadOnlySemanticDocumented pins the invariant that
// AllowedPaths field's doc comment must describe READ access only,
// never write. A drive-by edit that swaps "may read" for "may write"
// (or drops the phrase) would silently broaden the sandbox grant; this
// test makes that regression loud.
func TestAllowedPaths_ReadOnlySemanticDocumented(t *testing.T) {
	data, err := os.ReadFile("sandbox.go")
	if err != nil {
		t.Fatalf("read sandbox.go: %v", err)
	}
	src := string(data)
	idx := strings.Index(src, "AllowedPaths []string")
	if idx < 0 {
		t.Fatal("AllowedPaths field declaration not found in sandbox.go — regression")
	}
	// Walk back up to the preceding blank line and collect the doc-comment block.
	commentBlock := src[:idx]
	if nl := strings.LastIndex(commentBlock, "\n\n"); nl >= 0 {
		commentBlock = commentBlock[nl:]
	}
	if !strings.Contains(commentBlock, "may read") {
		t.Fatalf("AllowedPaths doc comment must contain \"may read\" to pin read-only semantics; got:\n%s",
			commentBlock)
	}
	if strings.Contains(commentBlock, "may write") {
		t.Fatalf("AllowedPaths doc comment must NOT say \"may write\" — regression:\n%s",
			commentBlock)
	}
}
