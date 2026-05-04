//go:build !cgo && !test_harness

// Package gateway — no-op stub for the test harness (non-test_harness builds).
//
// This file is compiled in every build that does NOT set the test_harness tag,
// i.e. all production and normal test builds. It provides a no-op
// registerTestHarness so the call site in registerAdditionalEndpoints compiles
// without conditional compilation directives.
//
// In a non-test_harness build:
//   - No HTTP endpoint is registered for /api/v1/_test/scenario.
//   - No provider override is installed.
//   - The HarnessQueue type does not exist; nothing references it at runtime.
//
// The call site in rest.go's registerAdditionalEndpoints is:
//
//	a.registerTestHarness(cm)
//
// In production this is a pure no-op with zero overhead.

package gateway

// registerTestHarness is a no-op in non-test_harness builds.
// The HTTP endpoint POST /api/v1/_test/scenario is not registered.
func (a *restAPI) registerTestHarness(_ httpHandlerRegistrar) {}
