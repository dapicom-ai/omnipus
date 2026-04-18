//go:build !cgo

// Package perf contains Go benchmarks and SLO gate tests for Plan 3 PR-C.
//
// TestMain registers the real gateway.RunContext and provider-override hooks
// into pkg/agent/testutil so that StartTestGateway can boot the full gateway
// without creating an import cycle.
//
// All benchmark files in this package share this TestMain.
package perf

import (
	"os"
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/agent/testutil"
	"github.com/dapicom-ai/omnipus/pkg/gateway"
)

func TestMain(m *testing.M) {
	testutil.RegisterGatewayRunner(gateway.RunContext)
	testutil.RegisterProviderOverrideFuncs(
		gateway.SetTestProviderOverride,
		gateway.ClearTestProviderOverride,
	)
	os.Exit(m.Run())
}
