// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

//go:build test

// V2.B: testAutoApproveApprover preserves the historical auto-approve
// behaviour that was the package-default before V2.B, but ONLY under the
// `test` build tag. Tests that exercise the runtime ask-policy gate must
// install this approver explicitly via AgentLoop.SetToolApprover. The
// migration makes the test's dependency on auto-approval visible — a
// production binary built without `-tags test` will never link this type
// and cannot accidentally skip the human-in-the-loop gate.
//
// Closes silent-failure-hunter BE CRIT-1: previously the default
// nopPolicyApprover returned (true, "") with a doc-comment justifying it
// as a "safe fallback for unit tests and CLI mode" — exactly the
// rationalisation that lets a fail-open into production.

package agent

import "context"

// testAutoApproveApprover unconditionally approves every PolicyApprover
// request. Use only in tests that need to exercise post-approval execution
// paths without standing up the full gateway approval-registry plumbing.
type testAutoApproveApprover struct{}

// RequestApproval returns (true, "test_auto_approve") for every request.
// The non-empty denialReason on approve is unused by the agent loop
// (denialReason is only consumed when approved == false), but the explicit
// "test_auto_approve" string is included for diagnostic clarity if a future
// loop change starts logging it on the approve path.
func (testAutoApproveApprover) RequestApproval(_ context.Context, _ PolicyApprovalReq) (bool, string) {
	return true, "test_auto_approve"
}
