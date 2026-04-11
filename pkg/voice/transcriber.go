package voice

import (
	"context"
	"log/slog"
	"strings"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/credentials"
	"github.com/dapicom-ai/omnipus/pkg/providers"
)

type Transcriber interface {
	Name() string
	Transcribe(ctx context.Context, audioFilePath string) (*TranscriptionResponse, error)
}

type TranscriptionResponse struct {
	Text     string  `json:"text"`
	Language string  `json:"language,omitempty"`
	Duration float64 `json:"duration,omitempty"`
}

func supportsAudioTranscription(model string) bool {
	protocol, _ := providers.ExtractProtocol(model)

	switch protocol {
	case "openai", "azure", "azure-openai",
		"litellm", "openrouter", "groq", "zhipu", "gemini", "nvidia",
		"ollama", "moonshot", "shengsuanyun", "deepseek", "cerebras",
		"vivgrid", "volcengine", "vllm", "qwen", "qwen-intl", "qwen-international", "dashscope-intl",
		"qwen-us", "dashscope-us", "mistral", "avian", "minimax", "longcat", "modelscope", "novita",
		"coding-plan", "alibaba-coding", "qwen-coding":
		// These protocols all go through the OpenAI-compatible or Azure provider path in
		// providers.CreateProviderFromConfig, so they are the only ones that can supply
		// the audio media payload shape expected by NewAudioModelTranscriber.

		// TODO: Further restrict this by modelID, since not every model under these
		// protocols supports audio transcription.
		return true
	default:
		return false
	}
}

// DetectTranscriber inspects cfg and returns the appropriate Transcriber, or
// nil if no supported transcription provider is configured.
// secrets provides the resolved ElevenLabs API key without using os.Getenv.
func DetectTranscriber(cfg *config.Config, secrets credentials.SecretBundle) Transcriber {
	if modelName := strings.TrimSpace(cfg.Voice.ModelName); modelName != "" {
		modelCfg, err := cfg.GetModelConfig(modelName)
		if err != nil {
			slog.Warn("voice: configured transcription model not found in providers — transcription unavailable",
				"voice.model_name", modelName, "error", err)
			return nil
		}
		if supportsAudioTranscription(modelCfg.Model) {
			return NewAudioModelTranscriber(modelCfg)
		}
	}

	// ElevenLabs voice config (supports Scribe STT). Key is resolved from the
	// SecretBundle so child processes never see it via /proc/<pid>/environ.
	if key := strings.TrimSpace(secrets.GetString(cfg.Voice.ElevenLabsAPIKeyRef)); key != "" {
		return NewElevenLabsTranscriber(key)
	}
	// Fall back to any model-list entry that uses the groq/ protocol.
	for _, mc := range cfg.Providers {
		if strings.HasPrefix(mc.Model, "groq/") && mc.APIKey() != "" {
			return NewGroqTranscriber(mc.APIKey())
		}
	}
	return nil
}
