//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/providers"
)

// HandleCompleteOnboarding handles POST /api/v1/onboarding/complete.
// It performs three steps:
//  1. Stores the API key in the encrypted credentials store (if available).
//  2. Atomically adds/updates the provider entry and creates the admin user
//     in config.json via safeUpdateConfigJSON.
//  3. Marks onboarding as complete in state.json.
//
// Steps 1-2 are best-effort atomic: if config write succeeds but state.json
// save fails, the admin already exists. Re-calling with the same username is
// idempotent — it updates hashes and retries the state save.
func (a *restAPI) HandleCompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Check if onboarding already complete.
	if a.onboardingMgr.IsComplete() {
		jsonErr(w, http.StatusConflict, "onboarding already complete")
		return
	}

	var body struct {
		Provider struct {
			ID     string `json:"id"`
			APIKey string `json:"api_key"`
			Model  string `json:"model"`
		} `json:"provider"`
		Admin struct {
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Validate provider.
	if body.Provider.ID == "" {
		jsonErr(w, http.StatusBadRequest, "provider.id is required")
		return
	}
	// Reject unknown protocols at the boundary so the gateway does not persist
	// a config that will fail the post-save rewire and flip to degraded.
	if !providers.IsKnownProtocol(body.Provider.ID) {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("provider.id %q is not a known protocol", body.Provider.ID))
		return
	}
	if body.Provider.APIKey == "" {
		jsonErr(w, http.StatusBadRequest, "provider.api_key is required")
		return
	}

	// Validate admin.
	if body.Admin.Username == "" {
		jsonErr(w, http.StatusBadRequest, "admin.username is required")
		return
	}
	if body.Admin.Password == "" {
		jsonErr(w, http.StatusBadRequest, "admin.password is required")
		return
	}
	if len(body.Admin.Password) < 8 {
		jsonErr(w, http.StatusBadRequest, "admin.password must be at least 8 characters")
		return
	}

	// Store the API key in the encrypted credentials store (AES-256-GCM).
	// Refuses the operation if the store is locked (SEC-23: no plaintext fallback).
	credRefName, credErr := a.storeCredential(body.Provider.ID+"_API_KEY", body.Provider.APIKey)
	if credErr != nil {
		slog.Error("rest: credential store unavailable during onboarding", "error", credErr)
		jsonErr(
			w,
			http.StatusServiceUnavailable,
			"credential store locked: set OMNIPUS_MASTER_KEY or unlock before saving secrets",
		)
		return
	}

	// Build the provider entry as a JSON object to inject into providers array.
	// model defaults per provider when not specified in the onboarding request.
	providerModel := body.Provider.Model
	if providerModel == "" {
		switch body.Provider.ID {
		case "anthropic":
			providerModel = "claude-sonnet-4-6"
		case "gemini", "google":
			providerModel = "gemini-2.0-flash"
		case "openrouter":
			providerModel = "openai/gpt-4o"
		default: // openai and any other provider
			providerModel = "gpt-4o"
		}
	}
	newProviderEntry := map[string]any{
		"model_name":  body.Provider.ID,
		"provider":    body.Provider.ID,
		"model":       providerModel,
		"api_key_ref": credRefName,
	}

	// Pre-compute all expensive crypto operations outside the config lock to
	// avoid holding configMu for ~300ms across three bcrypt operations.
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(body.Admin.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("onboarding: bcrypt password hash failed", "error", err)
		jsonErr(w, http.StatusInternalServerError, "onboarding failed")
		return
	}
	token, err := generateUserToken(body.Admin.Username)
	if err != nil {
		slog.Error("onboarding: generate token failed", "error", err)
		jsonErr(w, http.StatusInternalServerError, "onboarding failed")
		return
	}
	tokenHash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("onboarding: bcrypt token hash failed", "error", err)
		jsonErr(w, http.StatusInternalServerError, "onboarding failed")
		return
	}

	// Use safeUpdateConfigJSON to atomically:
	// 1. Add/update provider in providers array
	// 2. Register admin user
	// Only after both succeed, call CompleteOnboarding().
	if err := a.safeUpdateConfigJSON(func(m map[string]any) error {
		// Re-check inside the lock to prevent TOCTOU race where concurrent
		// requests all pass the IsComplete() check before any marks it done.
		if a.onboardingMgr.IsComplete() {
			return fmt.Errorf("onboarding already complete")
		}
		// --- Provider ---
		providerList, ok := m["providers"].([]any)
		if !ok {
			if m["providers"] != nil {
				return fmt.Errorf("providers field is not an array: %T", m["providers"])
			}
			providerList = []any{}
		}

		// Check if provider already exists; update or append.
		found := false
		for i, entry := range providerList {
			entryMap, isMap := entry.(map[string]any)
			if !isMap {
				continue
			}
			if entryMap["model_name"] == body.Provider.ID || entryMap["model"] == body.Provider.ID {
				// Update existing entry.
				if credRefName != "" {
					entryMap["api_key_ref"] = credRefName
					delete(entryMap, "api_key")
					delete(entryMap, "api_keys")
				} else {
					entryMap["api_key"] = body.Provider.APIKey
				}
				if body.Provider.Model != "" {
					entryMap["model"] = body.Provider.Model
				}
				entryMap["provider"] = body.Provider.ID
				providerList[i] = entryMap
				found = true
				break
			}
		}
		if !found {
			providerList = append(providerList, newProviderEntry)
		}
		m["providers"] = providerList

		// --- Set default model ---
		// The provider's model_name becomes the default agent model so the
		// gateway doesn't start in limited mode after onboarding.
		agentsMap, ok := m["agents"].(map[string]any)
		if !ok {
			agentsMap = map[string]any{}
		}
		defaultsMap, ok := agentsMap["defaults"].(map[string]any)
		if !ok {
			defaultsMap = map[string]any{}
		}
		defaultsMap["model_name"] = body.Provider.ID
		agentsMap["defaults"] = defaultsMap
		m["agents"] = agentsMap

		// --- Admin user ---
		// Build the user entry using pre-computed hashes.
		newUser := map[string]any{
			"username":      body.Admin.Username,
			"password_hash": string(passwordHash),
			"token_hash":    string(tokenHash),
			"role":          "admin",
		}

		// Ensure gateway object exists in m.
		if m["gateway"] == nil {
			m["gateway"] = map[string]any{}
		}
		gatewayMap, ok := m["gateway"].(map[string]any)
		if !ok {
			return fmt.Errorf("gateway config is not a map")
		}
		users := make([]any, 0, 1)
		if raw, exists := gatewayMap["users"]; exists {
			var ok bool
			users, ok = raw.([]any)
			if !ok {
				return fmt.Errorf("gateway.users is not an array")
			}
		}
		// Check for duplicate username. If the same admin already exists (e.g.,
		// from a partial commit where config was saved but state.json wasn't),
		// treat as idempotent success: overwrite the hashes so the caller gets
		// a working session with the newly generated token.
		for _, u := range users {
			um, ok := u.(map[string]any)
			if !ok {
				continue
			}
			if um["username"] == body.Admin.Username {
				um["password_hash"] = string(passwordHash)
				um["token_hash"] = string(tokenHash)
				return nil
			}
		}
		users = append(users, newUser)
		gatewayMap["users"] = users
		m["gateway"] = gatewayMap
		return nil
	}); err != nil {
		if err.Error() == "onboarding already complete" {
			jsonErr(w, http.StatusConflict, "onboarding already complete")
			return
		}
		slog.Error("onboarding: complete transaction failed", "error", err)
		jsonErr(w, http.StatusInternalServerError, "onboarding failed")
		return
	}

	// Config saved successfully. Trigger a reload so in-memory config picks up the new user.
	a.awaitReload()

	// Mark onboarding complete.
	if err := a.onboardingMgr.CompleteOnboarding(); err != nil {
		slog.Error("onboarding: CompleteOnboarding failed after config persisted", "error", err)
		jsonErr(w, http.StatusInternalServerError, "onboarding state save failed")
		return
	}

	slog.Info("onboarding: completed", "username", body.Admin.Username)
	resp := map[string]any{
		"token":    token,
		"role":     config.UserRoleAdmin,
		"username": body.Admin.Username,
	}
	if credRefName == "" {
		resp["warning"] = "API key stored in plaintext — set OMNIPUS_MASTER_KEY for encrypted storage"
	}
	jsonOK(w, resp)
}
