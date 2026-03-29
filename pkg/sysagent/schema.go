// Omnipus — System Agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package sysagent

import (
	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// IsCloudProvider returns true for known cloud LLM providers that receive
// summarized tool schemas (per BRD Wave 5b spec §US-5 / Appendix D §D.8.2).
// Local providers (Ollama, etc.) receive full schemas.
func IsCloudProvider(providerName string) bool {
	switch providerName {
	case "anthropic", "openai", "deepseek", "groq", "openrouter",
		"azure", "bedrock", "cohere", "mistral", "gemini":
		return true
	}
	return false
}

// SummarizedSchema returns a minimal tool definition for cloud providers:
// tool name, one-line description, and parameter names only.
// Reduces prompt size from ~10-15K to ~2-3K tokens.
func SummarizedSchema(t tools.Tool) map[string]any {
	params := t.Parameters()
	paramNames := extractParamNames(params)
	return map[string]any{
		"name":        t.Name(),
		"description": firstLine(t.Description()),
		"parameters":  paramNames,
	}
}

// extractParamNames extracts only the parameter names from a JSON Schema object.
func extractParamNames(params map[string]any) []string {
	props, ok := params["properties"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	return names
}

// firstLine returns the first non-empty line of s (used to extract the
// one-line summary from a multi-line tool description).
func firstLine(s string) string {
	for i, ch := range s {
		if ch == '\n' {
			return s[:i]
		}
	}
	return s
}

// FullSchema returns a complete flat tool definition (name, description, parameters)
// for local providers. Unlike ToolToSchema it does not wrap in a "function" envelope.
func FullSchema(t tools.Tool) map[string]any {
	return map[string]any{
		"name":        t.Name(),
		"description": t.Description(),
		"parameters":  t.Parameters(),
	}
}

// BuildToolSchemas returns tool definitions for the LLM.
// When cloudProvider is true, schemas are summarized (US-5 / §D.8.2).
// When cloudProvider is false (local), full schemas are returned.
func BuildToolSchemas(toolList []tools.Tool, cloudProvider bool) []map[string]any {
	schemas := make([]map[string]any, 0, len(toolList))
	for _, t := range toolList {
		if cloudProvider {
			schemas = append(schemas, SummarizedSchema(t))
		} else {
			schemas = append(schemas, FullSchema(t))
		}
	}
	return schemas
}
