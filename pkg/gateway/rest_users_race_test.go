//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/stretchr/testify/require"
)

// TestHandleUserChangeRole_ConcurrentDemotion_OneWins proves that the
// last-admin guard in HandleUserChangeRole is evaluated INSIDE the
// safeUpdateConfigJSON callback (post-configMu-acquire), not against a
// pre-lock snapshot.
//
// Setup per iteration: exactly two admins (alice, bob), zero non-admin users.
// Race: goroutine A demotes alice→user; goroutine B demotes bob→user.
// Both demotions would leave the deployment admin-less — but only the first
// to acquire configMu can commit; the second sees the post-write on-disk
// state (zero admins remaining after its own demote) and returns 409.
//
// Invariant across all 100 iterations:
//
//	success200  == 100  (exactly one 200 per iteration)
//	conflict409 == 100  (exactly one 409 per iteration)
//
// The test is deterministic by construction: with the guard inside the write
// lock the two outcomes are always {200,409} — never {200,200} or {409,409}.
func TestHandleUserChangeRole_ConcurrentDemotion_OneWins(t *testing.T) {
	const iterations = 100

	var success200, conflict409 int

	for i := 0; i < iterations; i++ {
		// Per-iteration fresh harness so each run starts from a clean
		// two-admin config on disk.
		hash, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
		require.NoError(t, err)
		users := []any{
			map[string]any{"username": "alice", "password_hash": string(hash), "token_hash": "", "role": "admin"},
			map[string]any{"username": "bob", "password_hash": string(hash), "token_hash": "", "role": "admin"},
		}
		api, _ := newUserMgmtAPI(t, users)

		codes := make(chan int, 2)
		var wg sync.WaitGroup
		wg.Add(2)

		// Goroutine A: demote alice to user.
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			r := adminRequest(http.MethodPatch, "/api/v1/users/alice/role", `{"role":"user"}`)
			api.HandleUserChangeRole(w, r)
			codes <- w.Code
		}()

		// Goroutine B: demote bob to user.
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			r := adminRequest(http.MethodPatch, "/api/v1/users/bob/role", `{"role":"user"}`)
			api.HandleUserChangeRole(w, r)
			codes <- w.Code
		}()

		wg.Wait()
		close(codes)

		codeA := <-codes
		codeB := <-codes

		got200, got409 := 0, 0
		for _, c := range []int{codeA, codeB} {
			switch c {
			case http.StatusOK:
				got200++
			case http.StatusConflict:
				got409++
			}
		}

		if got200 != 1 || got409 != 1 {
			t.Fatalf("iteration %d: expected one 200 + one 409, got codes %d and %d",
				i+1, codeA, codeB)
		}

		// On-disk config must have exactly one admin left.
		disk := readDiskUsers(t, api)
		adminCount := 0
		for _, u := range disk {
			if u["role"] == "admin" {
				adminCount++
			}
		}
		if adminCount != 1 {
			t.Fatalf("iteration %d: expected exactly 1 admin on disk after race, got %d (users: %+v)",
				i+1, adminCount, disk)
		}

		success200++
		conflict409++
	}

	// Aggregate assertion: every iteration must have contributed exactly one
	// success and one conflict. Deviations would have caused t.Fatalf above,
	// but this final check documents the invariant explicitly.
	require.Equal(t, iterations, success200, "success200 count must equal iteration count")
	require.Equal(t, iterations, conflict409, "conflict409 count must equal iteration count")

	t.Logf("ConcurrentDemotion: success200=%d conflict409=%d (all %d iterations correct)",
		success200, conflict409, iterations)
}

// TestHandleUserChangeRole_ConcurrentDemotion_OnDiskBodyContainsZeroAdmins
// is a complementary assertion: verifies that the 409 response body carries
// the canonical ErrLastAdmin message, not a generic 500.
//
// Runs a single iteration (not 100) — the error message is deterministic.
func TestHandleUserChangeRole_ConcurrentDemotion_ConflictBodyMessage(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	require.NoError(t, err)
	users := []any{
		map[string]any{"username": "alice", "password_hash": string(hash), "token_hash": "", "role": "admin"},
		map[string]any{"username": "bob", "password_hash": string(hash), "token_hash": "", "role": "admin"},
	}
	api, _ := newUserMgmtAPI(t, users)

	codes := make(chan int, 2)
	bodies := make(chan string, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		w := httptest.NewRecorder()
		api.HandleUserChangeRole(w, adminRequest(http.MethodPatch, "/api/v1/users/alice/role", `{"role":"user"}`))
		codes <- w.Code
		bodies <- w.Body.String()
	}()

	go func() {
		defer wg.Done()
		w := httptest.NewRecorder()
		api.HandleUserChangeRole(w, adminRequest(http.MethodPatch, "/api/v1/users/bob/role", `{"role":"user"}`))
		codes <- w.Code
		bodies <- w.Body.String()
	}()

	wg.Wait()
	close(codes)
	close(bodies)

	var bodySlice []string
	for b := range bodies {
		bodySlice = append(bodySlice, b)
	}

	var codeSlice []int
	for c := range codes {
		codeSlice = append(codeSlice, c)
	}

	found409 := false
	for i, c := range codeSlice {
		if c == http.StatusConflict {
			found409 = true
			var resp map[string]any
			require.NoError(t, json.Unmarshal([]byte(bodySlice[i]), &resp),
				"409 body must be valid JSON: %s", bodySlice[i])
			errMsg, _ := resp["error"].(string)
			require.Equal(t, ErrLastAdmin.Error(), errMsg,
				"409 body error must equal ErrLastAdmin sentinel; got: %s", bodySlice[i])
		}
	}
	require.True(t, found409, "at least one request must return 409 in the concurrent demotion scenario")
}
