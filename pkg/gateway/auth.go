// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// checkBearerAuth validates the Authorization header against OMNIPUS_BEARER_TOKEN.
// If the env var is unset, all requests are allowed (development mode).
// Returns false and writes a 401 if the token is set but invalid.
func checkBearerAuth(w http.ResponseWriter, r *http.Request) bool {
	required := os.Getenv("OMNIPUS_BEARER_TOKEN")
	if required == "" {
		return true // auth not configured
	}
	auth := r.Header.Get("Authorization")
	prefix := "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		http.Error(w, "unauthorized: missing Bearer token", http.StatusUnauthorized)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, prefix)), []byte(required)) != 1 {
		http.Error(w, "unauthorized: invalid Bearer token", http.StatusUnauthorized)
		return false
	}
	return true
}
