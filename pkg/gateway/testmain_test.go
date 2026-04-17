//go:build !cgo

package gateway

// TestMain registers the real gateway.RunContext and provider-override hooks
// into pkg/agent/testutil so that StartTestGateway can boot the full gateway
// without creating an import cycle (testutil does not import pkg/gateway).

import (
	"os"
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/agent/testutil"
)

func TestMain(m *testing.M) {
	// Register the real RunContext so StartTestGateway can call it.
	testutil.RegisterGatewayRunner(RunContext)

	// Register the provider override hooks so StartTestGateway can inject
	// a ScenarioProvider without the harness importing pkg/gateway.
	testutil.RegisterProviderOverrideFuncs(
		SetTestProviderOverride,
		ClearTestProviderOverride,
	)

	os.Exit(m.Run())
}
