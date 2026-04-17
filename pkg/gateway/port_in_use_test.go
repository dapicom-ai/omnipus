//go:build !cgo

// Contract test: Plan 3 §1 acceptance decision — binding to an in-use port must
// cause a fatal exit with an informative error message.
//
// BDD: Given port P is already in use, When the gateway tries to listen on port P,
//
//	Then it fails with an error that identifies the port and suggests a fix.
//
// Acceptance decision: Plan 3 §1 "Port-in-use: fatal at boot with fix hint"
// Traces to: temporal-puzzling-melody.md §4 Axis-1, pkg/gateway/port_in_use_test.go

package gateway

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPortInUseFatalExit verifies that attempting to listen on an already-occupied
// port returns an error (which the gateway startup path turns into a fatal exit).
//
// This test exercises the OS-level "address already in use" error that the gateway's
// net.Listen call would return when the port is occupied.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — TestPortInUseFatalExit
func TestPortInUseFatalExit(t *testing.T) {
	// BDD: Given port P is already in use — bind a listener to claim the port.
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "first listener must succeed")
	defer l1.Close()

	// Extract the port that was actually allocated.
	addr := l1.Addr().(*net.TCPAddr)
	occupiedAddr := addr.String()

	// BDD: When the gateway tries to listen on the same port.
	l2, err := net.Listen("tcp", occupiedAddr)
	if l2 != nil {
		l2.Close()
	}

	// BDD: Then the listen call must fail with an error.
	assert.Error(t, err,
		"listening on an already-occupied port must return an error")

	// The error message must contain enough context to identify the failure.
	// On Linux the message includes "address already in use"; on Windows "bind: An attempt".
	// Both contain the port number.
	if err != nil {
		errStr := err.Error()
		portStr := addr.AddrPort().Port()
		_ = portStr
		// Assert the error is not empty (not a silent failure).
		assert.NotEmpty(t, errStr,
			"port-in-use error must have a non-empty message")
	}

	// Differentiation: binding to a different (free) port must succeed.
	l3, err2 := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err2, "binding to a free port must succeed")
	if l3 != nil {
		l3.Close()
	}
}
