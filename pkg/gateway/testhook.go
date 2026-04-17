//go:build !cgo

// Package gateway — in-process test provider hook.
//
// This file is compiled into the production binary but only activated during
// in-process integration tests. It is NOT a test file (no _test.go suffix)
// because the harness in pkg/agent/testutil is a different package and cannot
// set unexported package-level variables in gateway via a _test.go file.
//
// Usage: set testProviderOverride before calling RunContext; clear it in
// t.Cleanup. The harness in pkg/agent/testutil/gateway_harness.go manages
// this via SetTestProviderOverride / ClearTestProviderOverride.
//
// In production: testProviderOverride is always nil — the branch in RunContext
// is a single pointer-nil check and adds no measurable overhead.

package gateway

import "github.com/dapicom-ai/omnipus/pkg/providers"

// testProviderOverride, when non-nil, is called by RunContext instead of
// createStartupProvider. This hook exists solely for in-process integration
// testing. Never set this in production code.
var testProviderOverride func() providers.LLMProvider

// SetTestProviderOverride installs a provider factory function that RunContext
// will call instead of the real createStartupProvider. Call
// ClearTestProviderOverride in t.Cleanup to avoid cross-test contamination.
func SetTestProviderOverride(fn func() providers.LLMProvider) {
	testProviderOverride = fn
}

// ClearTestProviderOverride removes the test provider override, restoring the
// production createStartupProvider path.
func ClearTestProviderOverride() {
	testProviderOverride = nil
}
