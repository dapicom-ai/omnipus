//go:build !cgo && !test_harness

// Package gateway — true no-op stubs for the test provider hook.
//
// This file is compiled in all builds that do NOT set the test_harness tag —
// i.e., every normal `go build` and production `go test` invocation without
// -tags=test_harness.
//
// H2 defense-in-depth: the variable exists for compilation but
// SetTestProviderOverride is a genuine no-op — it never calls Store.
// Because nobody calls SetTestProviderOverride in production code, the
// pointer stays nil at all times and gateway.go:420's Load() always returns
// nil (the production createStartupProvider path is always taken).
//
// The live atomic.Pointer + Store in the old stub was a defense-in-depth
// gap: it allowed a test that linked against the production binary to
// install an override without the test_harness tag. With this stub, only
// builds tagged test_harness (via testhook.go) can actually install an
// override.

package gateway

import (
	"sync/atomic"

	"github.com/dapicom-ai/omnipus/pkg/providers"
)

// testProviderOverride is always nil in non-test_harness builds. Declared
// here so gateway.go:420 compiles in all !cgo builds.
var testProviderOverride atomic.Pointer[func() providers.LLMProvider]

// SetTestProviderOverride is a no-op in production builds (non-test_harness).
// The argument is intentionally discarded — no Store is called.
func SetTestProviderOverride(_ func() providers.LLMProvider) {}

// ClearTestProviderOverride is a no-op in production builds (non-test_harness).
func ClearTestProviderOverride() {}
