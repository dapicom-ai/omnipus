package agent

import (
	"fmt"
	"strings"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/providers"
)

func buildModelListResolver(cfg *config.Config) func(raw string) (string, bool) {
	ensureProtocol := func(model string) string {
		model = strings.TrimSpace(model)
		if model == "" {
			return ""
		}
		if strings.Contains(model, "/") {
			return model
		}
		return "openai/" + model
	}

	return func(raw string) (string, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" || cfg == nil {
			return "", false
		}

		if mc, err := cfg.GetModelConfig(raw); err == nil && mc != nil && strings.TrimSpace(mc.Model) != "" {
			return ensureProtocol(mc.Model), true
		}

		for i := range cfg.Providers {
			fullModel := strings.TrimSpace(cfg.Providers[i].Model)
			if fullModel == "" {
				continue
			}
			if fullModel == raw {
				return ensureProtocol(fullModel), true
			}
			_, modelID := providers.ExtractProtocol(fullModel)
			if modelID == raw {
				return ensureProtocol(fullModel), true
			}
		}

		// Fallback: the requested slug isn't registered as its own provider
		// entry (e.g. user picked "z-ai/glm-5v-turbo" from the live model list),
		// but a kindred provider (e.g. openrouter) is configured and accepts
		// arbitrary slugs. Reuse that provider's credentials and pass the slug
		// through verbatim. Without this, ParseModelRef would mis-split the slug
		// (treating "z-ai" as a provider) and the runtime would silently fall
		// back to the default model.
		for i := range cfg.Providers {
			provName := strings.TrimSpace(cfg.Providers[i].Provider)
			if provName == "" {
				continue
			}
			// Pass-through providers route by model slug (their API accepts any
			// slug their backend exposes).
			if isPassthroughProvider(provName, cfg.Providers[i].APIBase) {
				return provName + "/" + raw, true
			}
		}

		return "", false
	}
}

// isPassthroughProvider reports whether the given provider type forwards model
// slugs to its backend without per-slug registration. OpenRouter is the
// canonical example.
func isPassthroughProvider(provider, apiBase string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openrouter", "vivgrid":
		return true
	}
	return strings.Contains(strings.ToLower(apiBase), "openrouter.ai")
}

func resolveModelCandidates(
	cfg *config.Config,
	defaultProvider string,
	primary string,
	fallbacks []string,
) []providers.FallbackCandidate {
	return providers.ResolveCandidatesWithLookup(
		providers.ModelConfig{
			Primary:   primary,
			Fallbacks: fallbacks,
		},
		defaultProvider,
		buildModelListResolver(cfg),
	)
}

func resolvedCandidateModel(candidates []providers.FallbackCandidate, fallback string) string {
	if len(candidates) > 0 && strings.TrimSpace(candidates[0].Model) != "" {
		return candidates[0].Model
	}
	return fallback
}

func resolvedCandidateProvider(candidates []providers.FallbackCandidate, fallback string) string {
	if len(candidates) > 0 && strings.TrimSpace(candidates[0].Provider) != "" {
		return candidates[0].Provider
	}
	return fallback
}

func resolvedModelConfig(cfg *config.Config, modelName, workspace string) (*config.ModelConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	modelCfg, err := cfg.GetModelConfig(strings.TrimSpace(modelName))
	if err != nil {
		return nil, err
	}

	clone := *modelCfg
	if clone.Workspace == "" {
		clone.Workspace = workspace
	}

	return &clone, nil
}
