package judge

import (
	"math"
	"testing"
)

// ── NewScore ──────────────────────────────────────────────────────────────────

func TestNewScore_RejectsNaN(t *testing.T) {
	_, err := NewScore(math.NaN())
	if err == nil {
		t.Fatal("expected error for NaN, got nil")
	}
}

func TestNewScore_RejectsPositiveInf(t *testing.T) {
	_, err := NewScore(math.Inf(1))
	if err == nil {
		t.Fatal("expected error for +Inf, got nil")
	}
}

func TestNewScore_RejectsNegativeInf(t *testing.T) {
	_, err := NewScore(math.Inf(-1))
	if err == nil {
		t.Fatal("expected error for -Inf, got nil")
	}
}

func TestNewScore_RejectsBelowZero(t *testing.T) {
	_, err := NewScore(-0.1)
	if err == nil {
		t.Fatal("expected error for -0.1, got nil")
	}
}

func TestNewScore_RejectsAboveOne(t *testing.T) {
	_, err := NewScore(1.1)
	if err == nil {
		t.Fatal("expected error for 1.1, got nil")
	}
}

func TestNewScore_AcceptsZero(t *testing.T) {
	s, err := NewScore(0.0)
	if err != nil {
		t.Fatalf("unexpected error for 0.0: %v", err)
	}
	if float64(s) != 0.0 {
		t.Fatalf("expected 0.0, got %v", s)
	}
}

func TestNewScore_AcceptsOne(t *testing.T) {
	s, err := NewScore(1.0)
	if err != nil {
		t.Fatalf("unexpected error for 1.0: %v", err)
	}
	if float64(s) != 1.0 {
		t.Fatalf("expected 1.0, got %v", s)
	}
}

func TestNewScore_AcceptsMidRange(t *testing.T) {
	cases := []float64{0.0, 0.25, 0.5, 0.75, 1.0}
	for _, f := range cases {
		s, err := NewScore(f)
		if err != nil {
			t.Errorf("unexpected error for %v: %v", f, err)
			continue
		}
		if float64(s) != f {
			t.Errorf("roundtrip mismatch: input %v, got %v", f, float64(s))
		}
	}
}

// ── Parse / JSON unmarshal path ───────────────────────────────────────────────

func TestParse_ValidResponse(t *testing.T) {
	raw := `{"completion": 0.9, "tools": 0.8, "persona": 0.7, "safety": 1.0, "efficiency": 0.5, "reasoning": "good"}`
	scores, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if float64(scores.Completion) != 0.9 {
		t.Errorf("completion: expected 0.9, got %v", scores.Completion)
	}
	if float64(scores.Safety) != 1.0 {
		t.Errorf("safety: expected 1.0, got %v", scores.Safety)
	}
	if scores.Reasoning != "good" {
		t.Errorf("reasoning: expected 'good', got %q", scores.Reasoning)
	}
}

func TestParse_MissingRequiredKey(t *testing.T) {
	raw := `{"completion": 0.9, "tools": 0.8, "persona": 0.7, "safety": 1.0}`
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected error for missing 'efficiency' key, got nil")
	}
}

func TestParse_NonNumericKey(t *testing.T) {
	raw := `{"completion": "high", "tools": 0.8, "persona": 0.7, "safety": 1.0, "efficiency": 0.5}`
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected error for non-numeric 'completion', got nil")
	}
}

func TestParse_MarkdownCodeFence(t *testing.T) {
	raw := "```json\n{\"completion\": 0.9, \"tools\": 0.8, \"persona\": 0.7, \"safety\": 1.0, \"efficiency\": 0.5, \"reasoning\": \"great\"}\n```"
	scores, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error parsing fenced JSON: %v", err)
	}
	if float64(scores.Completion) != 0.9 {
		t.Errorf("completion: expected 0.9, got %v", scores.Completion)
	}
}

func TestParse_ClampsSlightlyOutOfRange(t *testing.T) {
	// Some models return 1.0000001 due to floating-point; it should be clamped.
	raw := `{"completion": 1.0000001, "tools": -0.0000001, "persona": 0.5, "safety": 0.5, "efficiency": 0.5}`
	scores, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error for slightly out-of-range values: %v", err)
	}
	if float64(scores.Completion) != 1.0 {
		t.Errorf("completion should be clamped to 1.0, got %v", scores.Completion)
	}
	if float64(scores.Tools) != 0.0 {
		t.Errorf("tools should be clamped to 0.0, got %v", scores.Tools)
	}
}

func TestParse_NoJSON(t *testing.T) {
	_, err := Parse("no json here")
	if err == nil {
		t.Fatal("expected error for response with no JSON, got nil")
	}
}

func TestParse_EmptyReasoning(t *testing.T) {
	raw := `{"completion": 0.5, "tools": 0.5, "persona": 0.5, "safety": 0.5, "efficiency": 0.5}`
	scores, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scores.Reasoning != "" {
		t.Errorf("expected empty reasoning, got %q", scores.Reasoning)
	}
}
