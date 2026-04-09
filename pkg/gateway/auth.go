//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

var warnUnauthOnce sync.Once

// configContextKey is used to store a snapshotted *config.Config in the request
// context. This ensures all handlers within a single request see a consistent
// config even during hot-reload, preventing the race where the config pointer
// is replaced mid-iteration (~5-10% auth failures under concurrent load).
type configContextKey struct{}

// configFromContext retrieves the config snapshot stored by configSnapshotMiddleware.
// Returns nil if no snapshot was stored (caller should fall back to GetConfig()).
func configFromContext(ctx context.Context) *config.Config {
	cfg, _ := ctx.Value(configContextKey{}).(*config.Config)
	return cfg
}

// RoleContextKey is the context key for storing the authenticated user's role.
type RoleContextKey struct{}

// UserContextKey is the context key for storing the authenticated user config.
type UserContextKey struct{}

// AuthResult holds the outcome of a bearer token check.
type AuthResult struct {
	Authenticated bool
	Role          config.UserRole
	User          *config.UserConfig
}

// checkBearerAuth validates the Authorization header.
// It first checks config.Users (per-user RBAC), then falls back to the legacy
// OMNIPUS_BEARER_TOKEN env var (treated as admin role) for backward compatibility.
// Returns AuthResult so callers can distinguish no-auth from no-role.
func checkBearerAuth(ctx context.Context, w http.ResponseWriter, r *http.Request, cfg *config.Config) AuthResult {
	auth := r.Header.Get("Authorization")
	prefix := "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		// No Bearer prefix — treat as unauthenticated.
		http.Error(w, "unauthorized: missing Bearer token", http.StatusUnauthorized)
		return AuthResult{Authenticated: false}
	}
	rawToken := strings.TrimPrefix(auth, prefix)

	// 1. Check per-user list (RBAC — token hash lookup with bcrypt).
	if len(cfg.Gateway.Users) > 0 {
		for _, user := range cfg.Gateway.Users {
			if err := bcrypt.CompareHashAndPassword([]byte(user.TokenHash), []byte(rawToken)); err == nil {
				// Token matches this user.
				return AuthResult{Authenticated: true, Role: user.Role, User: &user}
			}
		}
		// Token not found in user list — reject.
		http.Error(w, "unauthorized: invalid Bearer token", http.StatusUnauthorized)
		return AuthResult{Authenticated: false}
	}

	// 2. Fallback: legacy OMNIPUS_BEARER_TOKEN env var (treated as admin role).
	required := os.Getenv("OMNIPUS_BEARER_TOKEN")
	if required == "" {
		if cfg.Gateway.DevModeBypass {
			// Auth not configured — development mode. Warn once at startup.
			warnUnauthOnce.Do(func() {
				slog.Warn("DEV MODE: API has no authentication. Set gateway.dev_mode_bypass=false for production.")
			})
			// Allow all requests in dev mode, treated as admin.
			return AuthResult{Authenticated: true, Role: config.UserRoleAdmin}
		}
		// No auth configured — deny by default (fail closed).
		http.Error(w, "unauthorized: no users configured, complete onboarding first", http.StatusUnauthorized)
		return AuthResult{Authenticated: false}
	}
	if subtle.ConstantTimeCompare([]byte(rawToken), []byte(required)) != 1 {
		http.Error(w, "unauthorized: invalid Bearer token", http.StatusUnauthorized)
		return AuthResult{Authenticated: false}
	}
	return AuthResult{Authenticated: true, Role: config.UserRoleAdmin}
}

// MapUserRoleToPrincipal converts a config.UserRole (human user) to the equivalent
// RBAC principal role string for agent operations. Fails closed: unknown roles
// get minimal "user" permissions.
func MapUserRoleToPrincipal(ur config.UserRole) string {
	switch ur {
	case config.UserRoleAdmin:
		return "admin"
	case config.UserRoleUser:
		return "user"
	default:
		return "user"
	}
}

// configSnapshotMiddleware snapshots the current config into the request context
// so all handlers in the same request see a consistent config. This prevents a
// race condition during hot-reload where the config pointer can be replaced
// mid-iteration, causing auth failures under concurrent load.
func (a *restAPI) configSnapshotMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := a.agentLoop.GetConfig()
		ctx := context.WithValue(r.Context(), configContextKey{}, cfg)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
