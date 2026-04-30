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
	// Defensively assert no neighboring field was introduced that would
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

// --- SandboxProfile UnmarshalJSON ---

// TestSandboxProfile_UnmarshalJSON_ValidValues confirms the canonical profile
// strings round-trip through UnmarshalJSON without error.
func TestSandboxProfile_UnmarshalJSON_ValidValues(t *testing.T) {
	cases := []struct {
		raw  string
		want SandboxProfile
	}{
		{`"workspace"`, SandboxProfileWorkspace},
		{`"workspace+net"`, SandboxProfileWorkspaceNet},
		{`"host"`, SandboxProfileHost},
		{`"off"`, SandboxProfileOff},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			var got SandboxProfile
			if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSandboxProfile_UnmarshalJSON_InvalidValueRejected confirms that unknown
// non-empty values are rejected at decode time so typos fail fast.
func TestSandboxProfile_UnmarshalJSON_InvalidValueRejected(t *testing.T) {
	var got SandboxProfile
	if err := json.Unmarshal([]byte(`"sandboxed"`), &got); err == nil {
		t.Fatal("expected error for unknown sandbox_profile value, got nil")
	}
}

// TestSandboxProfile_UnmarshalJSON_NoneRejected confirms that "none" is no
// longer a valid profile now that SandboxProfileNone has been removed.
func TestSandboxProfile_UnmarshalJSON_NoneRejected(t *testing.T) {
	var got SandboxProfile
	if err := json.Unmarshal([]byte(`"none"`), &got); err == nil {
		t.Fatal("expected error for removed 'none' sandbox_profile, got nil")
	}
}

// TestSandboxProfile_UnmarshalJSON_EmptyAccepted confirms that the empty
// string is accepted (config may legitimately omit this field).
func TestSandboxProfile_UnmarshalJSON_EmptyAccepted(t *testing.T) {
	var got SandboxProfile
	if err := json.Unmarshal([]byte(`""`), &got); err != nil {
		t.Fatalf("unexpected error for empty string: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestSandboxProfile_MarshalJSON_RoundTrip confirms that MarshalJSON produces
// the expected string representation and that Unmarshal recovers the value.
func TestSandboxProfile_MarshalJSON_RoundTrip(t *testing.T) {
	profiles := []SandboxProfile{
		SandboxProfileWorkspace,
		SandboxProfileWorkspaceNet,
		SandboxProfileHost,
		SandboxProfileOff,
	}
	for _, p := range profiles {
		t.Run(string(p), func(t *testing.T) {
			b, err := json.Marshal(p)
			if err != nil {
				t.Fatalf("MarshalJSON(%q) error: %v", p, err)
			}
			var got SandboxProfile
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("UnmarshalJSON(%s) error: %v", b, err)
			}
			if got != p {
				t.Errorf("round-trip: got %q, want %q", got, p)
			}
		})
	}
}

// TestSandboxProfile_IsValid confirms the IsValid predicate for known and
// unknown values including the empty string.
func TestSandboxProfile_IsValid(t *testing.T) {
	cases := []struct {
		p     SandboxProfile
		valid bool
	}{
		{SandboxProfileWorkspace, true},
		{SandboxProfileWorkspaceNet, true},
		{SandboxProfileHost, true},
		{SandboxProfileOff, true},
		{"", true},
		{"none", false}, // "none" was removed — must be invalid
		{"unknown", false},
		{"WORKSPACE", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.p), func(t *testing.T) {
			if got := tc.p.IsValid(); got != tc.valid {
				t.Errorf("IsValid(%q) = %v, want %v", tc.p, got, tc.valid)
			}
		})
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

// TestWorkspaceShellEnabled_DefaultFalse verifies deny-by-default: a nil pointer
// is filled with false by validateBootConfig, matching hard constraint #6.
func TestWorkspaceShellEnabled_DefaultFalse(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Sandbox.Experimental.WorkspaceShellEnabled = nil

	if err := validateBootConfig(cfg); err != nil {
		t.Fatalf("validateBootConfig: %v", err)
	}

	if cfg.Sandbox.Experimental.WorkspaceShellEnabled == nil {
		t.Fatal("expected WorkspaceShellEnabled to be non-nil after validateBootConfig")
	}
	if *cfg.Sandbox.Experimental.WorkspaceShellEnabled {
		t.Error("nil WorkspaceShellEnabled must default to false (deny-by-default), got true")
	}
}

// TestWorkspaceShellEnabled_ExplicitTrue verifies that an explicit true is preserved.
func TestWorkspaceShellEnabled_ExplicitTrue(t *testing.T) {
	cfg := DefaultConfig()
	v := true
	cfg.Sandbox.Experimental.WorkspaceShellEnabled = &v

	if err := validateBootConfig(cfg); err != nil {
		t.Fatalf("validateBootConfig: %v", err)
	}

	if cfg.Sandbox.Experimental.WorkspaceShellEnabled == nil || !*cfg.Sandbox.Experimental.WorkspaceShellEnabled {
		t.Error("explicit true must remain true after validateBootConfig")
	}
}

// TestWorkspaceShellEnabled_ExplicitFalse verifies that an explicit false is preserved.
func TestWorkspaceShellEnabled_ExplicitFalse(t *testing.T) {
	cfg := DefaultConfig()
	v := false
	cfg.Sandbox.Experimental.WorkspaceShellEnabled = &v

	if err := validateBootConfig(cfg); err != nil {
		t.Fatalf("validateBootConfig: %v", err)
	}

	if cfg.Sandbox.Experimental.WorkspaceShellEnabled == nil || *cfg.Sandbox.Experimental.WorkspaceShellEnabled {
		t.Error("explicit false must remain false after validateBootConfig")
	}
}
