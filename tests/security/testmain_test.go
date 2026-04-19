//go:build !cgo

// Package security_test — PR-D security test stack.
//
// TestMain registers the real gateway.RunContext and provider-override hooks
// into pkg/agent/testutil so that StartTestGateway can boot the full gateway
// without creating an import cycle.
//
// This file retains the //go:build !cgo tag because it imports pkg/gateway,
// which itself carries //go:build !cgo (the gateway uses modernc.org/sqlite and
// other pure-Go dependencies that conflict with CGO). The remaining test files in
// this package do NOT have the !cgo tag; they only test HTTP paths and do not
// import the gateway package directly. Under CGO_ENABLED=1, TestMain is excluded,
// so these tests must be run with CGO_ENABLED=0 or the goolm,stdjson tags.
//
// F1 context: the !cgo tag on the other test files was removed (F1 fix). This
// file is the sole exception because of the production gateway import.
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
