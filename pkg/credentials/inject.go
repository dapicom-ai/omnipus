// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package credentials

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// InjectFromConfig iterates over cfg.Providers entries, reads each entry's
// APIKeyRef field, resolves the referenced credential name from store, and
// injects the plaintext value into the process environment under that name.
//
// If store is locked or a referenced credential is missing, the affected
// provider fails to initialize with a descriptive error. Other providers
// continue. All errors are collected and returned as a slice.
//
// Implements US-11 acceptance criteria (SEC-22).
func InjectFromConfig(cfg *config.Config, store *Store) []error {
	if store.IsLocked() {
		return []error{ErrStoreLocked}
	}

	var errs []error
	injected := map[string]bool{} // avoid re-injecting duplicates

	for i := range cfg.Providers {
		model := cfg.Providers[i]
		ref := strings.TrimSpace(model.APIKeyRef)
		if ref == "" {
			continue
		}
		if injected[ref] {
			continue
		}

		value, err := store.Get(ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("provider %q: credential %q: %w", model.ModelName, ref, err))
			continue
		}

		if err := os.Setenv(ref, value); err != nil {
			errs = append(errs, fmt.Errorf("provider %q: set env %q: %w", model.ModelName, ref, err))
			continue
		}
		injected[ref] = true
		slog.Debug("credentials: injected", "ref", ref, "provider", model.ModelName)
	}

	return errs
}

// ResolveRef looks up a single credential reference and returns its plaintext.
// Returns a descriptive error if the store is locked or the ref is not found.
func ResolveRef(store *Store, ref string) (string, error) {
	if store.IsLocked() {
		return "", ErrStoreLocked
	}
	return store.Get(ref)
}

// channelRef associates a credential store reference with the channel and field
// that owns it. This lets the log message name the owning channel when a
// credential is missing, rather than just printing the ref name.
type channelRef struct {
	channel string // e.g. "telegram", "wecom", "voice.elevenlabs"
	field   string // e.g. "token_ref", "secret_ref", "api_key_ref"
	ref     string // the actual credential name, e.g. "TELEGRAM_TOKEN"
}

// InjectChannelsFromConfig resolves all channel *Ref credential fields in cfg and injects
// them into the process environment so that channel constructors can read them via os.Getenv.
// Missing credentials (ErrNotFound) are logged at Warn and returned in the error slice so that
// callers (boot path, reload) can decide whether the missing credential is fatal based on whether
// the channel is enabled. All errors are collected and returned.
func InjectChannelsFromConfig(cfg *config.Config, store *Store) []error {
	if store.IsLocked() {
		return []error{ErrStoreLocked}
	}

	ch := cfg.Channels
	refs := []channelRef{
		{channel: "telegram", field: "token_ref", ref: ch.Telegram.TokenRef},
		{channel: "discord", field: "token_ref", ref: ch.Discord.TokenRef},
		{channel: "slack", field: "bot_token_ref", ref: ch.Slack.BotTokenRef},
		{channel: "slack", field: "app_token_ref", ref: ch.Slack.AppTokenRef},
		{channel: "feishu", field: "app_secret_ref", ref: ch.Feishu.AppSecretRef},
		{channel: "feishu", field: "encrypt_key_ref", ref: ch.Feishu.EncryptKeyRef},
		{channel: "feishu", field: "verification_token_ref", ref: ch.Feishu.VerificationTokenRef},
		{channel: "qq", field: "app_secret_ref", ref: ch.QQ.AppSecretRef},
		{channel: "dingtalk", field: "client_secret_ref", ref: ch.DingTalk.ClientSecretRef},
		{channel: "matrix", field: "access_token_ref", ref: ch.Matrix.AccessTokenRef},
		{channel: "matrix", field: "crypto_passphrase_ref", ref: ch.Matrix.CryptoPassphraseRef},
		{channel: "line", field: "channel_secret_ref", ref: ch.LINE.ChannelSecretRef},
		{channel: "line", field: "channel_access_token_ref", ref: ch.LINE.ChannelAccessTokenRef},
		{channel: "onebot", field: "access_token_ref", ref: ch.OneBot.AccessTokenRef},
		{channel: "wecom", field: "secret_ref", ref: ch.WeCom.SecretRef},
		{channel: "weixin", field: "token_ref", ref: ch.Weixin.TokenRef},
		{channel: "irc", field: "password_ref", ref: ch.IRC.PasswordRef},
		{channel: "irc", field: "nickserv_password_ref", ref: ch.IRC.NickServPasswordRef},
		{channel: "irc", field: "sasl_password_ref", ref: ch.IRC.SASLPasswordRef},
		{channel: "voice.elevenlabs", field: "api_key_ref", ref: cfg.Voice.ElevenLabsAPIKeyRef},
		{channel: "tools.skills_github", field: "token_ref", ref: cfg.Tools.Skills.Github.TokenRef},
		{channel: "tools.skills_clawhub", field: "auth_token_ref", ref: cfg.Tools.Skills.Registries.ClawHub.AuthTokenRef},
		// Web tool credential refs (Brave, Tavily, Perplexity, GLM, Baidu).
		{channel: "tools.web_brave", field: "api_key_ref", ref: cfg.Tools.Web.Brave.APIKeyRef},
		{channel: "tools.web_tavily", field: "api_key_ref", ref: cfg.Tools.Web.Tavily.APIKeyRef},
		{channel: "tools.web_perplexity", field: "api_key_ref", ref: cfg.Tools.Web.Perplexity.APIKeyRef},
		{channel: "tools.web_glm", field: "api_key_ref", ref: cfg.Tools.Web.GLMSearch.APIKeyRef},
		{channel: "tools.web_baidu", field: "api_key_ref", ref: cfg.Tools.Web.BaiduSearch.APIKeyRef},
	}

	injected := map[string]bool{}
	var errs []error
	for _, cr := range refs {
		if cr.ref == "" || injected[cr.ref] {
			continue
		}
		value, err := store.Get(cr.ref)
		if err != nil {
			var notFound *ErrNotFound
			if errors.As(err, &notFound) {
				// Credential not stored — warn and return so the caller can decide
				// whether the missing credential is fatal (enabled channel) or safe
				// to skip (disabled channel / unconfigured tool).
				slog.Warn("credentials: channel credential not found",
					"channel", cr.channel,
					"field", cr.field,
					"ref", cr.ref,
				)
				errs = append(errs, fmt.Errorf("channel %q field %q credential %q: %w", cr.channel, cr.field, cr.ref, err))
				continue
			}
			errs = append(errs, fmt.Errorf("channel %q field %q credential %q: %w", cr.channel, cr.field, cr.ref, err))
			continue
		}
		if setErr := os.Setenv(cr.ref, value); setErr != nil {
			errs = append(errs, fmt.Errorf("channel %q field %q credential %q: set env: %w", cr.channel, cr.field, cr.ref, setErr))
			continue
		}
		injected[cr.ref] = true
		slog.Debug("credentials: injected channel credential", "channel", cr.channel, "field", cr.field, "ref", cr.ref)
	}
	return errs
}
