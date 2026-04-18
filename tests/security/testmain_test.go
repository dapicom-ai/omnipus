//go:build !cgo

// Package security_test — PR-D security test stack.
//
// TestMain registers the real gateway.RunContext and provider-override hooks
// into pkg/agent/testutil so that StartTestGateway can boot the full gateway
// without creating an import cycle.
//
// All tests in this package (xss_test.go, csrf_test.go, authz_matrix_test.go,
// credential_leakage_test.go, prompt_injection_levels_test.go, supply_chain_test.go,
// plus the axis-7 tests owned by D1) share this TestMain.
package security_test

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
