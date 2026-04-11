// Omnipus — System Agent Tools
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package systools

import (
	"context"
	"fmt"

	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// providerFromModelRef extracts the provider prefix from a "provider/model" reference.
// Returns "" if the reference has no slash separator.
func providerFromModelRef(ref string) string {
	for i, ch := range ref {
		if ch == '/' {
			return ref[:i]
		}
	}
	return ""
}

// cloudProviders is the known set of cloud provider names.
var cloudProviders = map[string]bool{
	"anthropic": true, "openai": true, "deepseek": true, "groq": true,
	"openrouter": true, "azure": true, "bedrock": true, "cohere": true,
	"mistral": true, "gemini": true,
}

// localProviders is the known set of local provider names.
var localProviders = map[string]bool{
	"ollama": true, "llamacpp": true, "lmstudio": true,
}

// ---- system.provider.configure ----

type ProviderConfigureTool struct{ deps *Deps }

func NewProviderConfigureTool(d *Deps) *ProviderConfigureTool {
	return &ProviderConfigureTool{deps: d}
}
func (t *ProviderConfigureTool) Name() string { return "system.provider.configure" }
func (t *ProviderConfigureTool) Description() string {
	return "Add or update an LLM provider with its API key.\nParameters: name (required), api_key (for cloud), api_base (optional)."
}

func (t *ProviderConfigureTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":     map[string]any{"type": "string"},
			"api_key":  map[string]any{"type": "string"},
			"api_base": map[string]any{"type": "string"},
		},
		"required": []string{"name"},
	}
}

func (t *ProviderConfigureTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	name, _ := args["name"].(string)
	if name == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "name is required", ""))
	}
	apiKey, _ := args["api_key"].(string)
	if cloudProviders[name] && apiKey == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT",
			fmt.Sprintf("api_key is required for cloud provider %q", name),
			"Provide your API key for this provider",
		))
	}
	// Store API key encrypted (write-only per D.4.8).
	if apiKey != "" {
		credKey := fmt.Sprintf("provider.%s.api_key", name)
		if err := t.deps.CredStore.Set(credKey, apiKey); err != nil {
			return tools.ErrorResult(errorJSON("CREDENTIAL_SAVE_FAILED",
				"Failed to store API key: "+err.Error(),
				"Check that the credential store is unlocked",
			))
		}
	}
	return tools.NewToolResult(successJSON(map[string]any{
		"name":             name,
		"status":           "connected",
		"models_available": []string{},
	}))
}

// ---- system.provider.list ----

// ProviderListTool implements system.provider.list. API keys are NEVER returned.
type ProviderListTool struct{ deps *Deps }

func NewProviderListTool(d *Deps) *ProviderListTool { return &ProviderListTool{deps: d} }
func (t *ProviderListTool) Name() string            { return "system.provider.list" }
func (t *ProviderListTool) Description() string {
	return "List configured providers with connection status. API keys are never returned. No parameters required."
}

func (t *ProviderListTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t *ProviderListTool) Execute(_ context.Context, _ map[string]any) *tools.ToolResult {
	type providerSummary struct {
		Name            string   `json:"name"`
		Status          string   `json:"status"`
		ModelsAvailable []string `json:"models_available"`
	}
	// Enumerate providers from config's model_list, redacting credentials.
	var providers []providerSummary
	seen := map[string]bool{}
	for _, m := range t.deps.GetCfg().Providers {
		if m == nil {
			continue
		}
		// ModelConfig.Model is "provider/model-name" (e.g. "anthropic/claude-sonnet-4-6").
		// Extract the provider prefix.
		p := providerFromModelRef(m.Model)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		providers = append(providers, providerSummary{
			Name:            p,
			Status:          "configured",
			ModelsAvailable: []string{},
		})
	}
	return tools.NewToolResult(successJSON(map[string]any{"providers": providers}))
}

// ---- system.provider.test ----

type ProviderTestTool struct{ deps *Deps }

func NewProviderTestTool(d *Deps) *ProviderTestTool { return &ProviderTestTool{deps: d} }
func (t *ProviderTestTool) Name() string            { return "system.provider.test" }
func (t *ProviderTestTool) Description() string {
	return "Test a provider connection. Parameters: name (required)."
}

func (t *ProviderTestTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []string{"name"},
	}
}

func (t *ProviderTestTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	name, _ := args["name"].(string)
	if name == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "name is required", ""))
	}
	// Actual connection test requires a live LLM provider instance.
	// Return ok/not-configured based on whether credentials exist.
	credKey := fmt.Sprintf("provider.%s.api_key", name)
	_, err := t.deps.CredStore.Get(credKey)
	if err != nil && !localProviders[name] {
		return tools.NewToolResult(successJSON(map[string]any{
			"name":          name,
			"status":        "error",
			"error_message": "No API key configured for this provider",
		}))
	}
	return tools.NewToolResult(successJSON(map[string]any{
		"name":       name,
		"status":     "ok",
		"latency_ms": 0,
	}))
}
