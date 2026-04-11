//go:build !cgo

// Package gateway — credential boot integration tests.
//
// These tests verify that the gateway boot path correctly wires
// credentials.InjectFromConfig and credentials.InjectChannelsFromConfig,
// so that provider and channel *Ref values are resolved into the process
// environment before any channel constructor or provider factory runs.
//
// Implements: BRD SEC-22 (encrypted credential store) / SEC-23 (deny-by-default).

package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/credentials"
)

// newUnlockedStore creates a credentials.Store in tmpDir, sets a random
// OMNIPUS_MASTER_KEY, unlocks the store, and returns it.
// The key environment variable is cleaned up when t finishes.
func newUnlockedStore(t *testing.T, tmpDir string) *credentials.Store {
	t.Helper()
	rawKey := make([]byte, 32)
	_, err := rand.Read(rawKey)
	require.NoError(t, err)
	t.Setenv("OMNIPUS_MASTER_KEY", hex.EncodeToString(rawKey))

	store := credentials.NewStore(tmpDir + "/credentials.json")
	require.NoError(t, credentials.Unlock(store))
	return store
}

// TestInjectFromConfig_InjectsProviderKey verifies that InjectFromConfig resolves
// a provider's APIKeyRef from the credential store and injects its value into the
// process environment under the ref name.
//
// BDD: Given a provider with APIKeyRef = "TEST_OPENAI_KEY",
//
//	AND "TEST_OPENAI_KEY" is stored in the credential store with value "sk-boottest",
//	When InjectFromConfig is called,
//	Then os.Getenv("TEST_OPENAI_KEY") == "sk-boottest".
func TestInjectFromConfig_InjectsProviderKey(t *testing.T) {
	tmpDir := t.TempDir()
	store := newUnlockedStore(t, tmpDir)

	const refName = "TEST_BOOT_OPENAI_KEY"
	const apiKeyValue = "sk-boot-test-value"

	// Store the credential.
	require.NoError(t, store.Set(refName, apiKeyValue))

	// Build a minimal config with one provider using APIKeyRef.
	cfg := &config.Config{
		Providers: config.SecureModelList{
			{ModelName: "openai", APIKeyRef: refName},
		},
	}

	// Ensure the env var is clean before we test injection.
	t.Setenv(refName, "")

	errs := credentials.InjectFromConfig(cfg, store)
	require.Empty(t, errs, "InjectFromConfig must not return errors: %v", errs)

	// The env var must now hold the injected value.
	assert.Equal(t, apiKeyValue, os.Getenv(refName),
		"InjectFromConfig must inject provider API key into process environment")

	// Cleanup: unset the env var so other tests are not affected.
	t.Cleanup(func() { os.Unsetenv(refName) })
}

// TestInjectChannelsFromConfig_InjectsChannelToken verifies that
// InjectChannelsFromConfig resolves a channel TokenRef from the credential
// store and injects its value into the process environment.
//
// BDD: Given Telegram.TokenRef = "TEST_BOOT_TG_TOKEN",
//
//	AND "TEST_BOOT_TG_TOKEN" is stored in the credential store with value "tg-tok-boot",
//	When InjectChannelsFromConfig is called,
//	Then os.Getenv("TEST_BOOT_TG_TOKEN") == "tg-tok-boot".
func TestInjectChannelsFromConfig_InjectsChannelToken(t *testing.T) {
	tmpDir := t.TempDir()
	store := newUnlockedStore(t, tmpDir)

	const refName = "TEST_BOOT_TG_TOKEN"
	const tokenValue = "tg-tok-boot"

	require.NoError(t, store.Set(refName, tokenValue))

	cfg := &config.Config{
		Channels: config.ChannelsConfig{
			Telegram: config.TelegramConfig{
				Enabled:  true,
				TokenRef: refName,
			},
		},
	}

	t.Setenv(refName, "")

	errs := credentials.InjectChannelsFromConfig(cfg, store)
	require.Empty(t, errs, "InjectChannelsFromConfig must not return errors: %v", errs)

	assert.Equal(t, tokenValue, os.Getenv(refName),
		"InjectChannelsFromConfig must inject channel token into process environment")

	t.Cleanup(func() { os.Unsetenv(refName) })
}

// TestInjectFromConfig_LockedStoreReturnsError verifies that InjectFromConfig
// returns ErrStoreLocked when the credential store has not been unlocked.
//
// BDD: Given a locked credential store,
//
//	When InjectFromConfig is called,
//	Then errs contains credentials.ErrStoreLocked.
func TestInjectFromConfig_LockedStoreReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	// Do NOT unlock — store remains locked.
	store := credentials.NewStore(tmpDir + "/credentials.json")

	cfg := &config.Config{
		Providers: config.SecureModelList{
			{ModelName: "anthropic", APIKeyRef: "ANTHROPIC_API_KEY"},
		},
	}

	errs := credentials.InjectFromConfig(cfg, store)
	require.Len(t, errs, 1, "locked store must produce exactly one error")
	assert.ErrorIs(t, errs[0], credentials.ErrStoreLocked,
		"error must be ErrStoreLocked when store is not unlocked")
}

// TestInjectChannelsFromConfig_LockedStoreReturnsError verifies that
// InjectChannelsFromConfig returns ErrStoreLocked when the store is locked.
func TestInjectChannelsFromConfig_LockedStoreReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	store := credentials.NewStore(tmpDir + "/credentials.json")

	cfg := &config.Config{
		Channels: config.ChannelsConfig{
			Telegram: config.TelegramConfig{
				Enabled:  true,
				TokenRef: "TELEGRAM_TOKEN",
			},
		},
	}

	errs := credentials.InjectChannelsFromConfig(cfg, store)
	require.Len(t, errs, 1, "locked store must produce exactly one error")
	assert.ErrorIs(t, errs[0], credentials.ErrStoreLocked,
		"error must be ErrStoreLocked when store is not unlocked")
}

// TestInjectChannelsFromConfig_MissingCredReturnsError verifies that
// a channel with a TokenRef that has no matching credential in the store
// returns an ErrNotFound error (not silently skipped) so the caller can
// decide whether the missing credential is fatal based on channel enabled state.
//
// BDD: Given Telegram.TokenRef = "TEST_BOOT_MISSING_REF",
//
//	AND the credential store does not contain "TEST_BOOT_MISSING_REF",
//	When InjectChannelsFromConfig is called,
//	Then errs contains an ErrNotFound for "TEST_BOOT_MISSING_REF",
//	AND the ref name appears in the error message,
//	AND os.Getenv("TEST_BOOT_MISSING_REF") is empty (no injection occurred).
func TestInjectChannelsFromConfig_MissingCredReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	store := newUnlockedStore(t, tmpDir)
	// Do NOT store any credential — ref is absent.

	const missingRef = "TEST_BOOT_MISSING_REF"

	cfg := &config.Config{
		Channels: config.ChannelsConfig{
			Telegram: config.TelegramConfig{
				Enabled:  true,
				TokenRef: missingRef,
			},
		},
	}

	// Ensure the env var is not set before calling.
	t.Setenv(missingRef, "")

	errs := credentials.InjectChannelsFromConfig(cfg, store)
	require.NotEmpty(t, errs, "missing credential must produce an error so the caller can decide if it's fatal")

	// The error must be or wrap ErrNotFound.
	var notFound *credentials.ErrNotFound
	found := false
	for _, e := range errs {
		if errors.As(e, &notFound) {
			found = true
			break
		}
	}
	assert.True(t, found, "error slice must contain an ErrNotFound for the missing ref")

	// The ref name must appear in at least one error message.
	foundRef := false
	for _, e := range errs {
		if assert.NotNil(t, e) && len(e.Error()) > 0 {
			if contains(e.Error(), missingRef) {
				foundRef = true
				break
			}
		}
	}
	assert.True(t, foundRef, "ref name %q must appear in the error message", missingRef)

	// No injection must have occurred — env var must remain empty.
	assert.Empty(t, os.Getenv(missingRef),
		"os.Getenv(%q) must be empty when credential is not found", missingRef)

	t.Cleanup(func() { os.Unsetenv(missingRef) })
}

// contains is a simple substring check used in tests.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// TestInjectFromConfig_EmptyRefSkipped verifies that providers with an empty
// APIKeyRef are skipped without error (they use a different auth mechanism or
// are public providers).
func TestInjectFromConfig_EmptyRefSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	store := newUnlockedStore(t, tmpDir)

	cfg := &config.Config{
		Providers: config.SecureModelList{
			{ModelName: "local-ollama", APIKeyRef: ""},
		},
	}

	errs := credentials.InjectFromConfig(cfg, store)
	assert.Empty(t, errs, "empty APIKeyRef must be skipped without error")
}

// TestInjectFromConfig_WebToolRefs verifies that web tool APIKeyRef values
// (Brave, Tavily, Perplexity, GLM, Baidu) are resolved and injected via
// InjectChannelsFromConfig.
func TestInjectFromConfig_WebToolRefs(t *testing.T) {
	tmpDir := t.TempDir()
	store := newUnlockedStore(t, tmpDir)

	refs := map[string]string{
		"TEST_BOOT_BRAVE_KEY":      "brave-test-val",
		"TEST_BOOT_TAVILY_KEY":     "tavily-test-val",
		"TEST_BOOT_PERPLEXITY_KEY": "perplexity-test-val",
		"TEST_BOOT_GLM_KEY":        "glm-test-val",
		"TEST_BOOT_BAIDU_KEY":      "baidu-test-val",
	}
	for ref, val := range refs {
		require.NoError(t, store.Set(ref, val), "store.Set(%q)", ref)
		t.Setenv(ref, "")
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Web: config.WebToolsConfig{
				Brave:      config.BraveConfig{APIKeyRef: "TEST_BOOT_BRAVE_KEY"},
				Tavily:     config.TavilyConfig{APIKeyRef: "TEST_BOOT_TAVILY_KEY"},
				Perplexity: config.PerplexityConfig{APIKeyRef: "TEST_BOOT_PERPLEXITY_KEY"},
				GLMSearch:  config.GLMSearchConfig{APIKeyRef: "TEST_BOOT_GLM_KEY"},
				BaiduSearch: config.BaiduSearchConfig{APIKeyRef: "TEST_BOOT_BAIDU_KEY"},
			},
		},
	}

	errs := credentials.InjectChannelsFromConfig(cfg, store)
	require.Empty(t, errs, "web tool injection must not return errors: %v", errs)

	for ref, expected := range refs {
		assert.Equal(t, expected, os.Getenv(ref),
			fmt.Sprintf("web tool ref %q must be injected into environment", ref))
	}

	t.Cleanup(func() {
		for ref := range refs {
			os.Unsetenv(ref)
		}
	})
}

// TestInjectChannelsFromConfig_AllChannelRefs is a table-driven test that covers
// every channel and web-tool ref field in InjectChannelsFromConfig. One row per
// ref ensures a typo in the inject.go slice is caught immediately.
//
// BDD: For each ref R in the complete inject list,
//
//	Given a store containing R=<expected-value>,
//	When InjectChannelsFromConfig is called with a config that sets R,
//	Then os.Getenv(R) == <expected-value>.
func TestInjectChannelsFromConfig_AllChannelRefs(t *testing.T) {
	type row struct {
		name   string        // human-readable label
		ref    string        // env-var / credential name
		setRef func(*config.Config, string) // patch cfg to use this ref
	}

	rows := []row{
		{"Telegram.TokenRef", "TBT_TG_TOKEN", func(c *config.Config, r string) { c.Channels.Telegram.TokenRef = r }},
		{"Discord.TokenRef", "TBT_DISCORD_TOKEN", func(c *config.Config, r string) { c.Channels.Discord.TokenRef = r }},
		{"Slack.BotTokenRef", "TBT_SLACK_BOT_TOKEN", func(c *config.Config, r string) { c.Channels.Slack.BotTokenRef = r }},
		{"Slack.AppTokenRef", "TBT_SLACK_APP_TOKEN", func(c *config.Config, r string) { c.Channels.Slack.AppTokenRef = r }},
		{"Feishu.AppSecretRef", "TBT_FEISHU_APP_SECRET", func(c *config.Config, r string) { c.Channels.Feishu.AppSecretRef = r }},
		{"Feishu.EncryptKeyRef", "TBT_FEISHU_ENCRYPT_KEY", func(c *config.Config, r string) { c.Channels.Feishu.EncryptKeyRef = r }},
		{"Feishu.VerificationTokenRef", "TBT_FEISHU_VERIF_TOKEN", func(c *config.Config, r string) { c.Channels.Feishu.VerificationTokenRef = r }},
		{"QQ.AppSecretRef", "TBT_QQ_APP_SECRET", func(c *config.Config, r string) { c.Channels.QQ.AppSecretRef = r }},
		{"DingTalk.ClientSecretRef", "TBT_DINGTALK_CLIENT_SECRET", func(c *config.Config, r string) { c.Channels.DingTalk.ClientSecretRef = r }},
		{"Matrix.AccessTokenRef", "TBT_MATRIX_ACCESS_TOKEN", func(c *config.Config, r string) { c.Channels.Matrix.AccessTokenRef = r }},
		{"Matrix.CryptoPassphraseRef", "TBT_MATRIX_CRYPTO_PASS", func(c *config.Config, r string) { c.Channels.Matrix.CryptoPassphraseRef = r }},
		{"LINE.ChannelSecretRef", "TBT_LINE_CHANNEL_SECRET", func(c *config.Config, r string) { c.Channels.LINE.ChannelSecretRef = r }},
		{"LINE.ChannelAccessTokenRef", "TBT_LINE_CHANNEL_ACCESS_TOKEN", func(c *config.Config, r string) { c.Channels.LINE.ChannelAccessTokenRef = r }},
		{"OneBot.AccessTokenRef", "TBT_ONEBOT_ACCESS_TOKEN", func(c *config.Config, r string) { c.Channels.OneBot.AccessTokenRef = r }},
		{"WeCom.SecretRef", "TBT_WECOM_SECRET", func(c *config.Config, r string) { c.Channels.WeCom.SecretRef = r }},
		{"Weixin.TokenRef", "TBT_WEIXIN_TOKEN", func(c *config.Config, r string) { c.Channels.Weixin.TokenRef = r }},
		{"IRC.PasswordRef", "TBT_IRC_PASSWORD", func(c *config.Config, r string) { c.Channels.IRC.PasswordRef = r }},
		{"IRC.NickServPasswordRef", "TBT_IRC_NICKSERV_PASSWORD", func(c *config.Config, r string) { c.Channels.IRC.NickServPasswordRef = r }},
		{"IRC.SASLPasswordRef", "TBT_IRC_SASL_PASSWORD", func(c *config.Config, r string) { c.Channels.IRC.SASLPasswordRef = r }},
		{"Voice.ElevenLabsAPIKeyRef", "TBT_ELEVENLABS_KEY", func(c *config.Config, r string) { c.Voice.ElevenLabsAPIKeyRef = r }},
		{"Skills.Github.TokenRef", "TBT_GITHUB_TOKEN", func(c *config.Config, r string) { c.Tools.Skills.Github.TokenRef = r }},
		{"Skills.ClawHub.AuthTokenRef", "TBT_CLAWHUB_AUTH_TOKEN", func(c *config.Config, r string) { c.Tools.Skills.Registries.ClawHub.AuthTokenRef = r }},
		{"Web.Brave.APIKeyRef", "TBT_BRAVE_KEY", func(c *config.Config, r string) { c.Tools.Web.Brave.APIKeyRef = r }},
		{"Web.Tavily.APIKeyRef", "TBT_TAVILY_KEY", func(c *config.Config, r string) { c.Tools.Web.Tavily.APIKeyRef = r }},
		{"Web.Perplexity.APIKeyRef", "TBT_PERPLEXITY_KEY", func(c *config.Config, r string) { c.Tools.Web.Perplexity.APIKeyRef = r }},
		{"Web.GLMSearch.APIKeyRef", "TBT_GLM_KEY", func(c *config.Config, r string) { c.Tools.Web.GLMSearch.APIKeyRef = r }},
		{"Web.BaiduSearch.APIKeyRef", "TBT_BAIDU_KEY", func(c *config.Config, r string) { c.Tools.Web.BaiduSearch.APIKeyRef = r }},
	}

	for _, tc := range rows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			store := newUnlockedStore(t, tmpDir)

			expectedValue := "val-" + tc.ref
			require.NoError(t, store.Set(tc.ref, expectedValue), "store.Set(%q)", tc.ref)

			cfg := &config.Config{}
			tc.setRef(cfg, tc.ref)

			t.Setenv(tc.ref, "")

			errs := credentials.InjectChannelsFromConfig(cfg, store)
			require.Empty(t, errs, "InjectChannelsFromConfig must not return errors for %q: %v", tc.name, errs)

			assert.Equal(t, expectedValue, os.Getenv(tc.ref),
				"ref %q (%s) must be injected into process environment", tc.ref, tc.name)

			t.Cleanup(func() { os.Unsetenv(tc.ref) })
		})
	}
}
