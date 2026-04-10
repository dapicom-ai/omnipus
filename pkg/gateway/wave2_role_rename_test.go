//go:build !cgo

// Wave 2 role-type rename tests.
//
// Verifies that:
//   - config.UserRole serialises/deserialises correctly as JSON strings "admin" / "user"
//   - config.UserRole.UnmarshalJSON rejects unknown role strings
//   - MapUserRoleToPrincipal maps admin → "admin", user → "user", unknown → "user"
//
// All tests are table-driven and include a differentiation case so that a
// hard-coded stub returning a constant value would be caught.

package gateway

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// TestUserRoleJSONRoundTrip verifies that UserRoleAdmin and UserRoleUser
// marshal to the expected JSON string values and unmarshal back intact.
//
// Traces to: wave2 role-type rename — config.UserRole constants UserRoleAdmin / UserRoleUser
// pkg/config/config.go line 835–836 (UserRoleAdmin = "admin", UserRoleUser = "user")
// pkg/config/config.go line 839–842 (MarshalJSON)
// pkg/config/config.go line 844–855 (UnmarshalJSON)
func TestUserRoleJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name         string
		role         config.UserRole
		expectedJSON string
	}{
		{
			name:         "admin role marshals to json string admin",
			role:         config.UserRoleAdmin,
			expectedJSON: `"admin"`,
		},
		{
			name:         "user role marshals to json string user",
			role:         config.UserRoleUser,
			expectedJSON: `"user"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// --- marshal ---
			got, err := json.Marshal(tc.role)
			require.NoError(t, err, "MarshalJSON must not return an error")
			assert.Equal(t, tc.expectedJSON, string(got), "marshaled JSON must match expected string literal")

			// --- unmarshal back (round-trip) ---
			var roundTripped config.UserRole
			err = json.Unmarshal(got, &roundTripped)
			require.NoError(t, err, "UnmarshalJSON must not return an error for a valid role")
			assert.Equal(t, tc.role, roundTripped, "round-tripped role must equal original")
		})
	}

	// Differentiation: the two roles must produce DIFFERENT JSON values.
	// A stub that always returns the same bytes would fail this check.
	adminJSON, _ := json.Marshal(config.UserRoleAdmin)
	userJSON, _ := json.Marshal(config.UserRoleUser)
	assert.NotEqual(t, string(adminJSON), string(userJSON),
		"UserRoleAdmin and UserRoleUser must marshal to different JSON values")
}

// TestUserRoleUnmarshalJSONRejectsInvalid verifies that UnmarshalJSON returns
// a non-nil error when presented with a role string that is not "admin" or "user".
//
// Traces to: wave2 role-type rename — config.UserRole.UnmarshalJSON validation
// pkg/config/config.go line 844–855 (default branch returns fmt.Errorf)
func TestUserRoleUnmarshalJSONRejectsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "unknown role string", input: `"invalid"`},
		{name: "superuser role is not defined", input: `"superuser"`},
		{name: "empty string role", input: `""`},
		{name: "numeric value", input: `1`},
		{name: "null value", input: `null`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r config.UserRole
			err := json.Unmarshal([]byte(tc.input), &r)
			assert.Error(t, err,
				"UnmarshalJSON must return an error for invalid role %q", tc.input)
		})
	}
}

// TestMapUserRoleToPrincipal verifies that MapUserRoleToPrincipal converts
// config.UserRole values to the correct RBAC principal string, and that
// unknown roles fail closed to the least-privileged "user" value.
//
// Traces to: wave2 role-type rename — MapUserRoleToPrincipal in pkg/gateway/auth.go
// pkg/gateway/auth.go line 101–110
func TestMapUserRoleToPrincipal(t *testing.T) {
	tests := []struct {
		name     string
		input    config.UserRole
		expected string
	}{
		{
			name:     "admin role maps to admin principal",
			input:    config.UserRoleAdmin,
			expected: "admin",
		},
		{
			name:     "user role maps to user principal",
			input:    config.UserRoleUser,
			expected: "user",
		},
		{
			name:     "unknown role fails closed to user principal",
			input:    config.UserRole("superuser"),
			expected: "user",
		},
		{
			name:     "empty role fails closed to user principal",
			input:    config.UserRole(""),
			expected: "user",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MapUserRoleToPrincipal(tc.input)
			assert.Equal(t, tc.expected, got,
				"MapUserRoleToPrincipal(%q) must return %q", string(tc.input), tc.expected)
		})
	}
}

// TestMapUserRoleToPrincipalDifferentiation verifies that admin and user produce
// DIFFERENT strings from MapUserRoleToPrincipal. A stub returning a constant
// value for all inputs would be caught here.
//
// Traces to: wave2 role-type rename — differentiation requirement
// pkg/gateway/auth.go line 101–110
func TestMapUserRoleToPrincipalDifferentiation(t *testing.T) {
	adminResult := MapUserRoleToPrincipal(config.UserRoleAdmin)
	userResult := MapUserRoleToPrincipal(config.UserRoleUser)

	assert.NotEqual(t, adminResult, userResult,
		"MapUserRoleToPrincipal must return different values for UserRoleAdmin (%q) and UserRoleUser (%q)",
		adminResult, userResult)

	// Also assert on the actual values, not just that they differ.
	assert.Equal(t, "admin", adminResult, "admin role must map to principal string \"admin\"")
	assert.Equal(t, "user", userResult, "user role must map to principal string \"user\"")
}
