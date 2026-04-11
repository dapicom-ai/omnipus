package model

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

var configPath = ""

func initTest(t *testing.T) {
	tmpDir := t.TempDir()
	configPath = filepath.Join(tmpDir, "config.json")
	_ = os.Setenv("OMNIPUS_CONFIG", configPath)
}

// captureStdout captures stdout during the execution of fn and returns the captured output
func captureStdout(fn func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestNewModelCommand(t *testing.T) {
	cmd := NewModelCommand()

	require.NotNil(t, cmd)

	assert.Equal(t, "model [model_name]", cmd.Use)
	assert.Equal(t, "Show or change the default model", cmd.Short)

	assert.Len(t, cmd.Aliases, 0)

	assert.False(t, cmd.HasFlags())

	assert.Nil(t, cmd.Run)
	assert.NotNil(t, cmd.RunE)

	assert.Nil(t, cmd.PersistentPreRunE)
	assert.Nil(t, cmd.PersistentPreRun)
	assert.Nil(t, cmd.PersistentPostRun)
}

func TestShowCurrentModel_WithDefaultModel(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName: "gpt-4",
			},
		},
		Providers: []*config.ModelConfig{
			{ModelName: "gpt-4", Model: "openai/gpt-4", APIKeyRef: "TEST_API_KEY"},
			{
				ModelName: "claude-3",
				Model:     "anthropic/claude-3",
				APIKeyRef: "TEST_API_KEY",
			},
		},
	}

	output := captureStdout(func() {
		showCurrentModel(cfg)
	})

	assert.Contains(t, output, "Current default model: gpt-4")
	assert.Contains(t, output, "Available models in your config:")
	assert.Contains(t, output, "gpt-4")
	assert.Contains(t, output, "claude-3")
}

func TestShowCurrentModel_NoDefaultModel(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName: "",
			},
		},
		Providers: []*config.ModelConfig{
			{ModelName: "gpt-4", Model: "openai/gpt-4", APIKeyRef: "TEST_API_KEY"},
		},
	}

	output := captureStdout(func() {
		showCurrentModel(cfg)
	})

	assert.Contains(t, output, "No default model is currently set.")
	assert.Contains(t, output, "Available models in your config:")
}

func TestListAvailableModels_Empty(t *testing.T) {
	cfg := &config.Config{
		Providers: []*config.ModelConfig{},
	}

	output := captureStdout(func() {
		listAvailableModels(cfg)
	})

	assert.Contains(t, output, "No providers configured in providers")
}

func TestListAvailableModels_WithModels(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName: "gpt-4",
			},
		},
		Providers: []*config.ModelConfig{
			{ModelName: "gpt-4", Model: "openai/gpt-4", APIKeyRef: "TEST_API_KEY"},
			{
				ModelName: "claude-3",
				Model:     "anthropic/claude-3",
				APIKeyRef: "TEST_API_KEY",
			},
			{ModelName: "no-key-model", Model: "openai/test"},
		},
	}

	output := captureStdout(func() {
		listAvailableModels(cfg)
	})

	assert.NotEmpty(t, output)
	assert.Contains(t, output, "> - gpt-4 (openai/gpt-4)")
	assert.Contains(t, output, "claude-3 (anthropic/claude-3)")
	assert.NotContains(t, output, "no-key-model")
}

func TestSetDefaultModel_ValidModel(t *testing.T) {
	initTest(t)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName: "old-model",
			},
		},
		Providers: []*config.ModelConfig{
			{
				ModelName: "new-model",
				Model:     "openai/new-model",
				APIKeyRef: "TEST_API_KEY",
			},
			{
				ModelName: "old-model",
				Model:     "openai/old-model",
				APIKeyRef: "TEST_API_KEY",
			},
		},
	}

	output := captureStdout(func() {
		err := setDefaultModel(configPath, cfg, "new-model")
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "Default model changed from 'old-model' to 'new-model'")

	// Verify config was updated
	updatedCfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	assert.Equal(t, "new-model", updatedCfg.Agents.Defaults.ModelName)
}

func TestSetDefaultModel_InvalidModel(t *testing.T) {
	initTest(t)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName: "existing-model",
			},
		},
		Providers: []*config.ModelConfig{
			{
				ModelName: "existing-model",
				Model:     "openai/existing",
				APIKeyRef: "TEST_API_KEY",
			},
		},
	}

	assert.Error(t, setDefaultModel(configPath, cfg, "nonexistent-model"))
}

func TestSetDefaultModel_ModelWithoutAPIKey(t *testing.T) {
	initTest(t)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName: "existing-model",
			},
		},
		Providers: []*config.ModelConfig{
			{
				ModelName: "existing-model",
				Model:     "openai/existing",
				APIKeyRef: "TEST_API_KEY",
			},
			{ModelName: "no-key-model", Model: "openai/nokey"},
		},
	}

	assert.Error(t, setDefaultModel(configPath, cfg, "no-key-model"))
}

func TestSetDefaultModel_SaveConfigError(t *testing.T) {
	// Use an invalid path to trigger save error
	invalidPath := "/nonexistent/directory/config.json"

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName: "old-model",
			},
		},
		Providers: []*config.ModelConfig{
			{
				ModelName: "new-model",
				Model:     "openai/new-model",
				APIKeyRef: "TEST_API_KEY",
			},
		},
	}

	err := setDefaultModel(invalidPath, cfg, "new-model")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save config")
}

func TestFormatModelName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", "(none)"},
		{"simple model", "gpt-4", "gpt-4"},
		{"model with version", "claude-sonnet-4.6", "claude-sonnet-4.6"},
		{"model with spaces", "my model", "my model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatModelName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestModelCommandExecution_Show(t *testing.T) {
	initTest(t)

	// Create a test config
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName: "test-model",
			},
		},
		Providers: []*config.ModelConfig{
			{
				ModelName: "test-model",
				Model:     "openai/test",
				APIKeyRef: "TEST_API_KEY",
			},
		},
	}

	err := config.SaveConfig(configPath, cfg)
	require.NoError(t, err)

	cmd := NewModelCommand()

	output := captureStdout(func() {
		err = cmd.RunE(cmd, []string{})
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "Current default model: test-model")
}

func TestModelCommandExecution_Set(t *testing.T) {
	initTest(t)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName: "old-model",
			},
		},
		Providers: []*config.ModelConfig{
			{
				ModelName: "old-model",
				Model:     "openai/old",
				APIKeyRef: "TEST_API_KEY",
			},
			{
				ModelName: "new-model",
				Model:     "openai/new",
				APIKeyRef: "TEST_API_KEY",
			},
		},
	}

	err := config.SaveConfig(configPath, cfg)
	require.NoError(t, err)

	cmd := NewModelCommand()

	output := captureStdout(func() {
		err = cmd.RunE(cmd, []string{"new-model"})
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "Default model changed from 'old-model' to 'new-model'")
}

func TestModelCommandExecution_TooManyArgs(t *testing.T) {
	cmd := NewModelCommand()

	err := cmd.RunE(cmd, []string{"model1", "model2"})

	assert.Error(t, err)
}

func TestListAvailableModels_MarkerLogic(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				ModelName: "middle-model",
			},
		},
		Providers: []*config.ModelConfig{
			{
				ModelName: "first-model",
				Model:     "openai/first",
				APIKeyRef: "TEST_API_KEY",
			},
			{
				ModelName: "middle-model",
				Model:     "openai/middle",
				APIKeyRef: "TEST_API_KEY",
			},
			{
				ModelName: "last-model",
				Model:     "openai/last",
				APIKeyRef: "TEST_API_KEY",
			},
		},
	}

	output := captureStdout(func() {
		listAvailableModels(cfg)
	})

	assert.Contains(t, output, "  - first-model (openai/first)")
	assert.Contains(t, output, "> - middle-model (openai/middle)")
	assert.Contains(t, output, "  - last-model (openai/last)")
}
