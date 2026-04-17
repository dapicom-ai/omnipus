package judge

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Scores holds the five numeric dimensions and free-text reasoning returned
// by the judge model.
type Scores struct {
	Completion float64 `json:"completion"`
	Tools      float64 `json:"tools"`
	Persona    float64 `json:"persona"`
	Safety     float64 `json:"safety"`
	Efficiency float64 `json:"efficiency"`
	Reasoning  string  `json:"reasoning"`
}

// codeBlockRe strips optional Markdown code-fence wrappers that some LLMs add
// around their JSON output, e.g. ```json\n{...}\n```.
var codeBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// extractJSON returns the first JSON object from s, stripping Markdown fences
// if present.
func extractJSON(s string) (string, error) {
	// Try code-fence first.
	if m := codeBlockRe.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1]), nil
	}
	// Fall back to the first { ... } in the raw string.
	start := strings.Index(s, "{")
	if start == -1 {
		return "", fmt.Errorf("judge response contains no JSON object")
	}
	// Walk forward to find the matching closing brace (handles nested objects
	// in the "reasoning" string value).
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if escaped {
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			if inStr {
				escaped = true
			}
		case '"':
			inStr = !inStr
		case '{':
			if !inStr {
				depth++
			}
		case '}':
			if !inStr {
				depth--
				if depth == 0 {
					return s[start : i+1], nil
				}
			}
		}
	}
	return "", fmt.Errorf("judge response contains an unclosed JSON object")
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Parse extracts a Scores value from the raw text returned by the judge model.
// It handles Markdown code fences, extracts the first JSON object, clamps each
// numeric score to [0, 1], and rejects responses with missing or non-numeric
// required keys.
func Parse(judgeResponse string) (Scores, error) {
	raw, err := extractJSON(judgeResponse)
	if err != nil {
		return Scores{}, fmt.Errorf("parse judge response: %w", err)
	}

	// Decode into a map first so we can detect missing vs zero-valued keys.
	var m map[string]json.RawMessage
	if err = json.Unmarshal([]byte(raw), &m); err != nil {
		return Scores{}, fmt.Errorf("parse judge JSON: %w", err)
	}

	requiredFloat := []string{"completion", "tools", "persona", "safety", "efficiency"}
	vals := make(map[string]float64, len(requiredFloat))
	for _, key := range requiredFloat {
		raw, ok := m[key]
		if !ok {
			return Scores{}, fmt.Errorf("judge response missing required key %q", key)
		}
		var v float64
		if err = json.Unmarshal(raw, &v); err != nil {
			return Scores{}, fmt.Errorf("judge response key %q is not a number: %w", key, err)
		}
		vals[key] = clamp(v)
	}

	var reasoning string
	if raw, ok := m["reasoning"]; ok {
		if err = json.Unmarshal(raw, &reasoning); err != nil {
			// Non-fatal: keep empty reasoning rather than rejecting the whole record.
			reasoning = ""
		}
	}

	return Scores{
		Completion: vals["completion"],
		Tools:      vals["tools"],
		Persona:    vals["persona"],
		Safety:     vals["safety"],
		Efficiency: vals["efficiency"],
		Reasoning:  reasoning,
	}, nil
}
