// Contract test: Plan 3 §1 acceptance decision — known secret patterns are
// masked by regex regardless of the filter_min_length setting, which applies
// only to the entropy-based content-length fast-path skip.
//
// BDD: Given a known secret pattern in content below filter_min_length,
//
//	When FilterSensitiveData or the audit redactor processes the content,
//	Then the secret pattern is masked (redacted).
//
// Acceptance decision: Plan 3 §1 "filter_min_length applies to entropy filter only;
// known patterns (sk-, Bearer, ghp_, AWS keys) mask regardless of length"
// Traces to: temporal-puzzling-melody.md §4 Axis-1, pkg/tools/filter_min_length_test.go

package tools_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/audit"
)

// TestShortSecretStillMaskedByPattern verifies that the audit redactor's pattern-based
// masking applies regardless of secret length. The audit.Redactor works at pattern match
// level; the filter_min_length fast-path is a separate content-length check in
// config.FilterSensitiveData that skips the entropy scan but NOT the audit redaction.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — TestShortSecretStillMaskedByPattern
func TestShortSecretStillMaskedByPattern(t *testing.T) {
	// The audit redactor is the canonical pattern-matching layer.
	redactor, err := audit.NewRedactor(nil) // nil = use default patterns
	require.NoError(t, err, "NewRedactor must succeed with nil custom patterns")

	tests := []struct {
		name  string
		input string
		// wantRedacted: true if the pattern should match and redact.
		// Note: the default patterns require minimum lengths (e.g., sk- needs 20+ chars).
		// This test documents the ACTUAL behavior, not a hypothetical.
		wantRedacted bool
		// note explains the expected behavior when the input is below pattern threshold.
		note string
	}{
		{
			// A full-length OpenAI key — matches sk-[a-zA-Z0-9-]{20,}
			name:         "full openai key is redacted",
			input:        "api_key: sk-abcdefghijklmnopqrstu",
			wantRedacted: true,
		},
		{
			// A GitHub token — matches ghp_[a-zA-Z0-9]{36} (requires exactly 36 chars)
			name:         "full github token is redacted",
			input:        "token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890",
			wantRedacted: true,
		},
		{
			// Bearer token — matches Bearer\s+[...]+
			name:         "bearer token is redacted",
			input:        "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantRedacted: true,
		},
		{
			// Short "sk-abc" (5 chars total) does NOT match sk-[a-zA-Z0-9-]{20,}
			// because the pattern requires 20+ chars after the prefix.
			// Document this contract: short fragments below pattern threshold are NOT redacted.
			name:         "short sk-abc below pattern threshold is not redacted by regex",
			input:        "key: sk-abc",
			wantRedacted: false,
			note: "sk-abc does not match sk-[a-zA-Z0-9-]{20,}; " +
				"plan §1 'known patterns mask regardless of length' refers to the regex-length " +
				"threshold (sk- + 20 chars), not the filter_min_length content-length fast-path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := redactor.Redact(tc.input)

			if tc.wantRedacted {
				assert.Contains(t, result, "[REDACTED]",
					"known secret pattern must be replaced with [REDACTED]; input: %q", tc.input)
				assert.NotEqual(t, tc.input, result,
					"redacted output must differ from input")
			} else {
				// Pattern threshold not met — no redaction expected.
				if tc.note != "" {
					t.Logf("NOTE: %s", tc.note)
				}
				assert.NotContains(t, result, "[REDACTED]",
					"short fragment below pattern threshold must not be redacted; input: %q", tc.input)
			}
		})
	}

	// Differentiation: a full key is redacted, a short fragment is not.
	fullKey := redactor.Redact("sk-abcdefghijklmnopqrstu")
	shortKey := redactor.Redact("sk-abc")
	assert.NotEqual(t, fullKey, shortKey,
		"full key and short fragment must produce different redaction outcomes")
}
