package judge

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Score is a validated float64 in the range [0, 1].
// NaN and Inf are rejected at construction time.
type Score float64

// NewScore validates f and returns a Score. Returns an error if f is NaN,
// infinite, or outside [0, 1].
func NewScore(f float64) (Score, error) {
	if math.IsNaN(f) {
		return 0, fmt.Errorf("score must not be NaN")
	}
	if math.IsInf(f, 0) {
		return 0, fmt.Errorf("score must not be infinite")
	}
	if f < 0 || f > 1 {
		return 0, fmt.Errorf("score %v is outside [0, 1]", f)
	}
	return Score(f), nil
}

// Scores holds the five numeric dimensions and free-text reasoning returned
// by the judge model.
type Scores struct {
	Completion Score  `json:"completion"`
	Tools      Score  `json:"tools"`
	Persona    Score  `json:"persona"`
	Safety     Score  `json:"safety"`
	Efficiency Score  `json:"efficiency"`
	Reasoning  string `json:"reasoning"`
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

// clamp clamps v to [0, 1]. Used when the judge returns a value that is
// only slightly out of range due to floating-point representation.
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// clampAndReport clamps v to [0,1] and reports whether any clamping happened.
// Callers decide whether the deviation is tolerable (FP noise) or a hard error
// (model returning out-of-contract values).
func clampAndReport(v float64) (clamped float64, wasClamped bool) {
	c := clamp(v)
	return c, c != v
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
	vals := make(map[string]Score, len(requiredFloat))
	for _, key := range requiredFloat {
		rawVal, ok := m[key]
		if !ok {
			return Scores{}, fmt.Errorf("judge response missing required key %q", key)
		}
		var f float64
		if err = json.Unmarshal(rawVal, &f); err != nil {
			return Scores{}, fmt.Errorf("judge response key %q is not a number: %w", key, err)
		}
		// Narrow FP noise (e.g. 1.0000001) is clamped and logged; anything
		// farther outside [0,1] is a broken judge and surfaced as an error
		// so silent masking can't hide model regressions.
		clamped, wasClamped := clampAndReport(f)
		if wasClamped && (f < -0.01 || f > 1.01) {
			return Scores{}, fmt.Errorf(
				"judge response key %q out of range [0,1]: got %g (refusing to silently clamp)",
				key, f,
			)
		}
		score, scoreErr := NewScore(clamped)
		if scoreErr != nil {
			return Scores{}, fmt.Errorf("judge response key %q invalid score: %w", key, scoreErr)
		}
		vals[key] = score
	}

	var reasoning string
	if rawVal, ok := m["reasoning"]; ok {
		if err = json.Unmarshal(rawVal, &reasoning); err != nil {
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
