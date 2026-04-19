//go:build !cgo

// Package perf contains Go benchmarks and SLO gate tests for Plan 3 PR-C.
//
// TestMain registers the real gateway.RunContext and provider-override hooks
// into pkg/agent/testutil so that StartTestGateway can boot the full gateway
// without creating an import cycle.
//
// This file retains //go:build !cgo because it imports pkg/gateway, which itself
// has //go:build !cgo (pure-Go modernc.org/sqlite). The other files in this package
// do NOT have the !cgo tag. Under CGO_ENABLED=1, TestMain is excluded and tests
// that call StartTestGateway will fail with a clear "gateway runner not registered"
// error rather than silently not running. Run with CGO_ENABLED=0 for full test
// execution (the standard development workflow).
//
// F1 context: !cgo was removed from all other perf test files. This file is the
// sole exception because it bridges the production gateway package.
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
