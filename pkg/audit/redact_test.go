// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package audit_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/audit"
)

// TestRedactionEngine_DefaultPatterns validates built-in redaction patterns remove
// API keys, GitHub PATs, email addresses, and bearer tokens from log strings.
// Traces to: wave2-security-layer-spec.md line 794 (TestRedactionEngine_DefaultPatterns)
// BDD: Scenario: API key pattern is redacted (spec line 601)
func TestRedactionEngine_DefaultPatterns(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 870 (Dataset: Redaction Patterns rows 1–6)
	redactor, err := audit.NewRedactor(nil) // nil = no custom patterns, use defaults
	require.NoError(t, err)

	tests := []struct {
		name         string
		input        string
		wantContains string
		wantMissing  string
	}{
		// Dataset row 1 — Anthropic API key format sk-ant-...
		{
			name:         "Anthropic API key sk-ant-... is redacted",
			input:        "sk-ant-abc123def456ghi789jkl012mno345",
			wantMissing:  "sk-ant-abc123def456ghi789jkl012mno345",
			wantContains: "[REDACTED]",
		},
		// Dataset row 2 — OpenAI project key sk-proj-...
		{
			name:         "OpenAI project key sk-proj-... is redacted",
			input:        "sk-proj-abc123def456ghi789jkl",
			wantMissing:  "sk-proj-",
			wantContains: "[REDACTED]",
		},
		// Dataset row 3 — GitHub PAT
		{
			name:         "GitHub PAT ghp_ is redacted",
			input:        "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			wantMissing:  "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			wantContains: "[REDACTED]",
		},
		// Dataset row 4 — email address (PII)
		{
			name:         "email address is redacted",
			input:        "user@example.com",
			wantMissing:  "user@example.com",
			wantContains: "[REDACTED]",
		},
		// Dataset row 5 — plain word not matched by patterns
		{
			name:         "plain 'password123' not matched",
			input:        "password123",
			wantContains: "password123", // no pattern matches a bare word without separator
		},
		// Dataset row 6 — JWT Bearer token
		{
			name:         "Bearer JWT token is redacted",
			input:        "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature",
			wantMissing:  "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			wantContains: "[REDACTED]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := redactor.Redact(tc.input)
			if tc.wantContains != "" {
				assert.Contains(t, got, tc.wantContains,
					"redacted output should contain %q", tc.wantContains)
			}
			if tc.wantMissing != "" {
				assert.NotContains(t, got, tc.wantMissing,
					"sensitive value %q should be removed from output", tc.wantMissing)
			}
		})
	}
}

// TestRedactionEngine_CustomPatterns validates user-defined regex patterns are applied
// in addition to default patterns.
// Traces to: wave2-security-layer-spec.md line 795 (TestRedactionEngine_CustomPatterns)
// BDD: Scenario: Custom redaction pattern (spec line 611)
func TestRedactionEngine_CustomPatterns(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 611 (Scenario: Custom redaction pattern)
	// Dataset row 7 — custom internal reference ID pattern
	customPatterns := []string{`INTERNAL-[0-9]{6}`}
	redactor, err := audit.NewRedactor(customPatterns)
	require.NoError(t, err)

	t.Run("custom pattern INTERNAL-123456 is redacted", func(t *testing.T) {
		got := redactor.Redact("INTERNAL-123456")
		assert.Equal(t, "[REDACTED]", got)
	})

	t.Run("custom pattern does not match wrong format (only 5 digits)", func(t *testing.T) {
		got := redactor.Redact("INTERNAL-12345")
		assert.Equal(t, "INTERNAL-12345", got,
			"6-digit pattern should not match 5 digits")
	})

	t.Run("custom patterns stack with default patterns", func(t *testing.T) {
		// Both custom AND default patterns should apply
		got := redactor.Redact("sk-ant-abc123def456ghi789jkl012mno345")
		assert.Contains(t, got, "[REDACTED]",
			"default API key pattern should still be active when custom patterns added")
		got2 := redactor.Redact("INTERNAL-123456")
		assert.Contains(t, got2, "[REDACTED]",
			"custom INTERNAL- pattern should also be active")
	})

	t.Run("invalid regex pattern returns error", func(t *testing.T) {
		// Traces to: wave2-security-layer-spec.md line 960 (invalid redaction pattern)
		_, err := audit.NewRedactor([]string{`[invalid regex`})
		require.Error(t, err, "invalid regex pattern must return an error")
		assert.Contains(t, err.Error(), "invalid redaction pattern")
	})
}

// TestRedactionEngine_Disabled validates passthrough behavior when redaction is turned off.
// Traces to: wave2-security-layer-spec.md line 796 (TestRedactionEngine_Disabled)
// BDD: Scenario: Redaction disabled (spec line 170, redaction: false)
func TestRedactionEngine_Disabled(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 170 (User Story 10, Acceptance Scenario 3)
	redactor := audit.DisabledRedactor()

	t.Run("API key passes through when redaction disabled", func(t *testing.T) {
		input := "sk-ant-abc123def456ghi789jkl012mno345"
		got := redactor.Redact(input)
		assert.Equal(t, input, got,
			"disabled redactor must return input unchanged")
	})

	t.Run("email passes through when redaction disabled", func(t *testing.T) {
		input := "user@example.com"
		got := redactor.Redact(input)
		assert.Equal(t, input, got)
	})

	t.Run("GitHub PAT passes through when redaction disabled", func(t *testing.T) {
		input := "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
		got := redactor.Redact(input)
		assert.Equal(t, input, got,
			"disabled redactor must pass through GitHub PATs unchanged")
	})
}

// TestRedactionEngine_RedactEntry validates that entries with sensitive data in parameters
// are redacted when logged with redaction enabled.
// Traces to: wave2-security-layer-spec.md line 794 (TestRedactionEngine_DefaultPatterns — entry-level)
// BDD: Scenario: API key in audit entry parameters is redacted (spec line 151)
func TestRedactionEngine_RedactEntry(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 151 (User Story 9, Acceptance Scenario 1 — redacted params)
	// NOTE: Entry-level redaction is applied internally by the Logger (redactEntry is not exported).
	// This test validates the full redaction pipeline through logger.Log().
	dir := t.TempDir()
	logger, err := audit.NewLogger(audit.LoggerConfig{
		Dir:           dir,
		RetentionDays: 90,
		RedactEnabled: true,
	})
	require.NoError(t, err)
	defer logger.Close()

	entry := audit.Entry{
		Timestamp: time.Now().UTC(),
		Event:     audit.EventToolCall,
		Decision:  "allow",
		AgentID:   "researcher",
		Tool:      "web_search",
		Parameters: map[string]any{
			"api_key": "sk-ant-abc123def456ghi789jkl012mno345",
			"query":   "safe query text",
		},
	}

	require.NoError(t, logger.Log(&entry))

	// Read back the written log entry and verify redaction was applied
	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))

	params, ok := parsed["parameters"].(map[string]any)
	require.True(t, ok, "parameters must be a JSON object")

	apiKey, _ := params["api_key"].(string)
	assert.NotContains(t, apiKey, "sk-ant-",
		"API key in parameters must be redacted")
	assert.Equal(t, "[REDACTED]", apiKey,
		"redacted parameter must become [REDACTED]")

	query, _ := params["query"].(string)
	assert.Equal(t, "safe query text", query,
		"non-sensitive parameter must be preserved unchanged")
}
