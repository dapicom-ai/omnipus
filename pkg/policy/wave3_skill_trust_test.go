package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEffectiveSkillTrust_BlockUnverified verifies that block_unverified policy
// returns SkillTrustBlockUnverified from EffectiveSkillTrust.
// Traces to: wave3-skill-ecosystem-spec.md line 838 (Test #8: TestTrustPolicyBlockUnverified)
// BDD: Given security.skill_trust = "block_unverified",
// When EffectiveSkillTrust() is called,
// Then SkillTrustBlockUnverified is returned.

func TestEffectiveSkillTrust_BlockUnverified(t *testing.T) {
	// Traces to: wave3-skill-ecosystem-spec.md line 447 (Scenario Outline: Install with different trust policies)
	sc := &SecurityConfig{SkillTrust: SkillTrustBlockUnverified}
	assert.Equal(t, SkillTrustBlockUnverified, sc.EffectiveSkillTrust(),
		"block_unverified must be returned as-is")
}

// TestEffectiveSkillTrust_WarnUnverified verifies that warn_unverified policy
// is returned correctly and is the default.
// Traces to: wave3-skill-ecosystem-spec.md line 839 (Test #9: TestTrustPolicyWarnUnverified)
// BDD: Given security.skill_trust = "warn_unverified" (or empty),
// When EffectiveSkillTrust() is called,
// Then SkillTrustWarnUnverified is returned.

func TestEffectiveSkillTrust_WarnUnverified(t *testing.T) {
	// Traces to: wave3-skill-ecosystem-spec.md line 447 (Scenario Outline)
	// Explicit setting
	sc := &SecurityConfig{SkillTrust: SkillTrustWarnUnverified}
	assert.Equal(t, SkillTrustWarnUnverified, sc.EffectiveSkillTrust(),
		"warn_unverified must be returned when explicitly set")

	// Default (empty string) also returns warn_unverified
	scDefault := &SecurityConfig{}
	assert.Equal(t, SkillTrustWarnUnverified, scDefault.EffectiveSkillTrust(),
		"warn_unverified must be the default when SkillTrust is empty")
}

// TestEffectiveSkillTrust_AllowAll verifies that allow_all bypasses verification.
// Traces to: wave3-skill-ecosystem-spec.md line 840 (Test #10: TestTrustPolicyAllowAll)
// BDD: Given security.skill_trust = "allow_all",
// When EffectiveSkillTrust() is called,
// Then SkillTrustAllowAll is returned.

func TestEffectiveSkillTrust_AllowAll(t *testing.T) {
	// Traces to: wave3-skill-ecosystem-spec.md line 447 (Scenario Outline)
	sc := &SecurityConfig{SkillTrust: SkillTrustAllowAll}
	assert.Equal(t, SkillTrustAllowAll, sc.EffectiveSkillTrust(),
		"allow_all must be returned as-is")
}

// TestParseSecurityConfig_SkillTrust verifies that ParseSecurityConfig correctly
// parses all three valid skill_trust values.
// Traces to: wave3-skill-ecosystem-spec.md line 838-840 (TDD plan tests #8-10)

func TestParseSecurityConfig_SkillTrust(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		wantTrust   SkillTrustPolicy
	}{
		{
			name:      "block_unverified",
			json:      `{"skill_trust": "block_unverified"}`,
			wantTrust: SkillTrustBlockUnverified,
		},
		{
			name:      "warn_unverified",
			json:      `{"skill_trust": "warn_unverified"}`,
			wantTrust: SkillTrustWarnUnverified,
		},
		{
			name:      "allow_all",
			json:      `{"skill_trust": "allow_all"}`,
			wantTrust: SkillTrustAllowAll,
		},
		{
			name:      "empty defaults to warn_unverified",
			json:      `{}`,
			wantTrust: SkillTrustWarnUnverified,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseSecurityConfig([]byte(tc.json))
			require.NoError(t, err)
			assert.Equal(t, tc.wantTrust, cfg.EffectiveSkillTrust())
		})
	}
}

// TestParseSecurityConfig_InvalidSkillTrust verifies that an invalid skill_trust
// value is rejected by ParseSecurityConfig.
// Traces to: wave3-skill-ecosystem-spec.md line 838-840 (validation edge case)

func TestParseSecurityConfig_InvalidSkillTrust(t *testing.T) {
	// Traces to: wave3-skill-ecosystem-spec.md line 190 (validateConfig invalid skill_trust check)
	_, err := ParseSecurityConfig([]byte(`{"skill_trust": "invalid_value"}`))
	require.Error(t, err, "invalid skill_trust must be rejected by ParseSecurityConfig")
	assert.Contains(t, err.Error(), "skill_trust",
		"error message must reference the invalid field name")
}
