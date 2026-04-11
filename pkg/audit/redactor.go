package audit

import (
	"fmt"
	"regexp"
)

// Default redaction patterns for SEC-16.
var defaultPatterns = []string{
	`sk-[a-zA-Z0-9\-]{20,}`,                            // OpenAI API keys
	`key-[a-zA-Z0-9]{20,}`,                             // Generic API keys
	`Bearer\s+[a-zA-Z0-9\-._~+/]+=*`,                   // Bearer tokens
	`ghp_[a-zA-Z0-9]{36}`,                              // GitHub personal access tokens
	`gho_[a-zA-Z0-9]{36}`,                              // GitHub OAuth tokens
	`xoxb-[0-9]{10,}-[a-zA-Z0-9]+`,                     // Slack bot tokens
	`xoxp-[0-9]{10,}-[a-zA-Z0-9]+`,                     // Slack user tokens
	`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`, // Email addresses
}

const redactedValue = "[REDACTED]"

// Redactor replaces sensitive patterns in audit log entries (SEC-16).
type Redactor struct {
	patterns []*regexp.Regexp
	enabled  bool
}

// NewRedactor creates a Redactor with default and optional custom patterns.
// Pass nil for customPatterns to use only default patterns.
// Returns an error if a custom pattern is invalid.
func NewRedactor(customPatterns []string) (*Redactor, error) {
	var patterns []*regexp.Regexp

	for _, p := range defaultPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			panic(fmt.Sprintf("BUG: invalid hardcoded redaction pattern %q: %v", p, err))
		}
		patterns = append(patterns, re)
	}

	for _, p := range customPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid redaction pattern %q: %w", p, err)
		}
		patterns = append(patterns, re)
	}

	return &Redactor{patterns: patterns, enabled: true}, nil
}

// DisabledRedactor returns a Redactor that passes through all values unchanged.
func DisabledRedactor() *Redactor {
	return &Redactor{enabled: false}
}

// Redact replaces all matching patterns in a string with [REDACTED].
func (r *Redactor) Redact(s string) string {
	if !r.enabled || len(r.patterns) == 0 {
		return s
	}
	for _, re := range r.patterns {
		s = re.ReplaceAllString(s, redactedValue)
	}
	return s
}

// redactMap recursively redacts string values in a map.
func (r *Redactor) redactMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = r.redactValue(v)
	}
	return result
}

func (r *Redactor) redactValue(v any) any {
	switch val := v.(type) {
	case string:
		return r.Redact(val)
	case map[string]any:
		return r.redactMap(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = r.redactValue(item)
		}
		return result
	default:
		return v
	}
}
