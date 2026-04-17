//go:build !cgo && test_harness

// Package gateway — in-process test provider hook (test_harness build only).
//
// This file is compiled ONLY when the test_harness build tag is present.
// Normal `go build` uses testhook_stub.go instead, which provides a nil-returning
// stub so gateway.go:307 compiles without conditional compilation.
//
// Usage: set testProviderOverride before calling RunContext; clear it in
// t.Cleanup. The harness in pkg/agent/testutil/gateway_harness.go manages
// this via SetTestProviderOverride / ClearTestProviderOverride.

package gateway

import (
	"sync/atomic"

	"github.com/dapicom-ai/omnipus/pkg/providers"
)

// testProviderOverride holds a pointer to a provider factory function.
// When non-nil, RunContext calls it instead of createStartupProvider.
// Using atomic.Pointer eliminates the data race between the goroutine that
// sets/clears the override and the goroutine running RunContext.
var testProviderOverride atomic.Pointer[func() providers.LLMProvider]

// SetTestProviderOverride installs a provider factory function that RunContext
// will call instead of the real createStartupProvider. Call
// ClearTestProviderOverride in t.Cleanup to avoid cross-test contamination.
func SetTestProviderOverride(fn func() providers.LLMProvider) {
	testProviderOverride.Store(&fn)
}

// ClearTestProviderOverride removes the test provider override, restoring the
// production createStartupProvider path.
func ClearTestProviderOverride() {
	testProviderOverride.Store(nil)
}
