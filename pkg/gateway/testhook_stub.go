//go:build !cgo && !test_harness

// Package gateway — production build of the test provider hook.
//
// This file is compiled in all builds that do NOT set the test_harness tag —
// i.e., every normal `go build` and `go test` invocation without
// -tags=test_harness. It provides the exact same atomic.Pointer var and
// Set/Clear functions as testhook.go so that:
//
//   - `go build` (production binary): the functions exist but are never called.
//     The atomic.Pointer is always nil, adding zero overhead to the hot path.
//   - `go test` (without test_harness): RegisterProviderOverrideFuncs in
//     pkg/agent/testutil/gateway_harness.go can still install the functions as
//     hooks, making the harness fully functional even without the tag.
//
// In a future hardening pass, the production binary can be stripped of these
// symbols by building with `-tags=test_harness` only in CI test steps and
// keeping the stub as a true no-op. For now, keeping the functions functional
// ensures the test suite passes under the standard build tags (goolm,stdjson).

package gateway

import (
	"sync/atomic"

	"github.com/dapicom-ai/omnipus/pkg/providers"
)

// testProviderOverride holds a pointer to a provider factory function.
// Nil in production (nobody calls SetTestProviderOverride outside tests).
var testProviderOverride atomic.Pointer[func() providers.LLMProvider]

// SetTestProviderOverride installs a provider factory for RunContext to use
// instead of the real createStartupProvider. Exported so testutil can install
// it via RegisterProviderOverrideFuncs without importing pkg/gateway.
// Safe to call concurrently (atomic.Pointer).
func SetTestProviderOverride(fn func() providers.LLMProvider) {
	testProviderOverride.Store(&fn)
}

// ClearTestProviderOverride removes the test provider override.
// Safe to call concurrently (atomic.Pointer).
func ClearTestProviderOverride() {
	testProviderOverride.Store(nil)
}
