package config

import (
	"testing"
)

// expandMultiKeyModels is a pass-through since multi-key expansion was removed
// in favour of credential-store-backed APIKeyRef. These tests verify the
// pass-through behaviour and that APIKey() resolves correctly from env.

func TestExpandMultiKeyModels_SingleKey(t *testing.T) {
	const keyRef = "MULTIKEY_TEST_SINGLE_KEY"
	t.Setenv(keyRef, "single-key")

	models := []*ModelConfig{
		{
			ModelName: "gpt-4",
			Model:     "openai/gpt-4o",
			APIKeyRef: keyRef,
		},
	}

	result := expandMultiKeyModels(models)

	if len(result) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result))
	}
	if result[0].ModelName != "gpt-4" {
		t.Errorf("expected model_name 'gpt-4', got %q", result[0].ModelName)
	}
	if result[0].APIKey() != "single-key" {
		t.Errorf("expected api_key 'single-key', got %q", result[0].APIKey())
	}
	if len(result[0].Fallbacks) != 0 {
		t.Errorf("expected no fallbacks, got %v", result[0].Fallbacks)
	}
}

func TestExpandMultiKeyModels_MultipleModels(t *testing.T) {
	const key1Ref = "MULTIKEY_TEST_KEY_1"
	const key2Ref = "MULTIKEY_TEST_KEY_2"
	const key3Ref = "MULTIKEY_TEST_KEY_3"
	t.Setenv(key1Ref, "key1")
	t.Setenv(key2Ref, "key2")
	t.Setenv(key3Ref, "key3")

	models := []*ModelConfig{
		{ModelName: "glm-4.7-a", Model: "zhipu/glm-4.7", APIBase: "https://api.example.com", APIKeyRef: key1Ref},
		{ModelName: "glm-4.7-b", Model: "zhipu/glm-4.7", APIBase: "https://api.example.com", APIKeyRef: key2Ref},
		{ModelName: "glm-4.7-c", Model: "zhipu/glm-4.7", APIBase: "https://api.example.com", APIKeyRef: key3Ref},
	}

	result := expandMultiKeyModels(models)

	if len(result) != 3 {
		t.Fatalf("expected 3 models (pass-through), got %d", len(result))
	}
	if result[0].APIKey() != "key1" {
		t.Errorf("result[0].APIKey() = %q, want %q", result[0].APIKey(), "key1")
	}
	if result[1].APIKey() != "key2" {
		t.Errorf("result[1].APIKey() = %q, want %q", result[1].APIKey(), "key2")
	}
	if result[2].APIKey() != "key3" {
		t.Errorf("result[2].APIKey() = %q, want %q", result[2].APIKey(), "key3")
	}
}

func TestExpandMultiKeyModels_PreservesOtherFields(t *testing.T) {
	const keyRef = "MULTIKEY_TEST_PRESERVE_KEY"
	t.Setenv(keyRef, "key0")

	modelCfg := &ModelConfig{
		ModelName:      "gpt-4",
		Model:          "openai/gpt-4o",
		APIBase:        "https://api.example.com",
		Proxy:          "http://proxy:8080",
		RPM:            60,
		MaxTokensField: "max_completion_tokens",
		RequestTimeout: 30,
		ThinkingLevel:  "high",
		APIKeyRef:      keyRef,
	}
	models := []*ModelConfig{modelCfg}

	result := expandMultiKeyModels(models)

	if len(result) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result))
	}
	primary := result[0]
	if primary.APIBase != "https://api.example.com" {
		t.Errorf("expected api_base preserved, got %q", primary.APIBase)
	}
	if primary.Proxy != "http://proxy:8080" {
		t.Errorf("expected proxy preserved, got %q", primary.Proxy)
	}
	if primary.RPM != 60 {
		t.Errorf("expected rpm preserved, got %d", primary.RPM)
	}
	if primary.MaxTokensField != "max_completion_tokens" {
		t.Errorf("expected max_tokens_field preserved, got %q", primary.MaxTokensField)
	}
	if primary.RequestTimeout != 30 {
		t.Errorf("expected request_timeout preserved, got %d", primary.RequestTimeout)
	}
	if primary.ThinkingLevel != "high" {
		t.Errorf("expected thinking_level preserved, got %q", primary.ThinkingLevel)
	}
}

func TestExpandMultiKeyModels_IsVirtualFlag(t *testing.T) {
	const keyRef = "MULTIKEY_TEST_VIRTUAL_KEY"
	t.Setenv(keyRef, "key1")

	models := []*ModelConfig{
		{
			ModelName: "gpt-4",
			Model:     "openai/gpt-4o",
			APIKeyRef: keyRef,
		},
	}

	result := expandMultiKeyModels(models)

	if len(result) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result))
	}
	// Non-expanded model should NOT be virtual
	if result[0].isVirtual {
		t.Errorf("model should not be virtual after pass-through expansion")
	}
	if result[0].IsVirtual() {
		t.Errorf("IsVirtual() should return false for non-virtual model")
	}
}

func TestMergeAPIKeys(t *testing.T) {
	tests := []struct {
		name     string
		apiKey   string
		apiKeys  []string
		expected []string
	}{
		{
			name:     "both empty",
			apiKey:   "",
			apiKeys:  nil,
			expected: nil,
		},
		{
			name:     "only ApiKey",
			apiKey:   "key1",
			apiKeys:  nil,
			expected: []string{"key1"},
		},
		{
			name:     "only ApiKeys",
			apiKey:   "",
			apiKeys:  []string{"key1", "key2"},
			expected: []string{"key1", "key2"},
		},
		{
			name:     "both with overlap",
			apiKey:   "key1",
			apiKeys:  []string{"key1", "key2", "key3"},
			expected: []string{"key1", "key2", "key3"},
		},
		{
			name:     "with whitespace",
			apiKey:   "  key1  ",
			apiKeys:  []string{"  key2  ", "  key1  "},
			expected: []string{"key1", "key2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeAPIKeys(tt.apiKey, tt.apiKeys)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d keys, got %d", len(tt.expected), len(result))
			}
			for i, k := range result {
				if k != tt.expected[i] {
					t.Errorf("expected key[%d] = %q, got %q", i, tt.expected[i], k)
				}
			}
		})
	}
}
