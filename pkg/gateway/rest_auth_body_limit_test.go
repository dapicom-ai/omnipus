//go:build !cgo

// B1.1 backend half: regression test for the 1 MiB body cap on
// withOptionalAuth-wrapped routes. An anonymous client cannot pin the
// gateway with an unbounded POST body.

package gateway

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithOptionalAuth_BodyLimit verifies that withOptionalAuth caps request
// bodies at 1 MiB. A 2 MiB POST must produce a *http.MaxBytesError when the
// downstream handler reads r.Body.
func TestWithOptionalAuth_BodyLimit(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	const overLimit = (1 << 20) + 1024 // 1 MiB + 1 KiB
	body := bytes.Repeat([]byte("a"), overLimit)

	var readErr error
	stub := func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		_ = r.Body.Close()
	}
	wrapped := api.withOptionalAuth(stub)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/state", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/octet-stream")
	wrapped(w, r)

	require.Error(t, readErr, "ReadAll must fail when the body exceeds the 1 MiB cap")
	var maxBytesErr *http.MaxBytesError
	require.True(t, errors.As(readErr, &maxBytesErr),
		"err must be *http.MaxBytesError, got %T: %v", readErr, readErr)
	require.Equal(t, int64(1<<20), maxBytesErr.Limit, "limit must be 1 MiB")
}

// TestWithOptionalAuth_UnderLimit confirms a body smaller than 1 MiB is
// allowed through unchanged — the cap is enforced strictly above the limit.
func TestWithOptionalAuth_UnderLimit(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	const underLimit = (1 << 20) - 1024 // 1 MiB - 1 KiB
	body := bytes.Repeat([]byte("b"), underLimit)

	var got []byte
	var readErr error
	stub := func(w http.ResponseWriter, r *http.Request) {
		got, readErr = io.ReadAll(r.Body)
		_ = r.Body.Close()
	}
	wrapped := api.withOptionalAuth(stub)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/state", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/octet-stream")
	wrapped(w, r)

	require.NoError(t, readErr)
	require.Equal(t, underLimit, len(got))
}
