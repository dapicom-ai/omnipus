package commands

import (
	"context"
	"testing"
)

// TestCancelDefinition_NotAliased asserts that the /cancel definition has no
// aliases. FR-5 forbids /stop, /abort, /kill, and any other alias. This test
// acts as the exact-set assertion from spec section 10 / T12a: future alias
// additions break this test loudly.
func TestCancelDefinition_NotAliased(t *testing.T) {
	defs := BuiltinDefinitions()

	var cancelDef *Definition
	for i := range defs {
		if defs[i].Name == "cancel" {
			d := defs[i]
			cancelDef = &d
			break
		}
	}
	if cancelDef == nil {
		t.Fatal("cancel definition not found in BuiltinDefinitions()")
	}

	if len(cancelDef.Aliases) != 0 {
		t.Errorf("/cancel must have no aliases (FR-5), got: %v", cancelDef.Aliases)
	}

	// Assert the registered Names across ALL definitions do not include any
	// forbidden alias names that could be mistaken for cancel.
	forbidden := []string{"stop", "abort", "kill"}
	for _, def := range defs {
		for _, alias := range def.Aliases {
			for _, f := range forbidden {
				if alias == f {
					t.Errorf("found forbidden alias %q on definition %q (FR-5)", alias, def.Name)
				}
			}
		}
		for _, f := range forbidden {
			if def.Name == f {
				t.Errorf("found forbidden command name %q — must not be registered (FR-5)", def.Name)
			}
		}
	}
}

// stubAgentLoop is a minimal AgentLoopInterface implementation used in tests.
type stubAgentLoop struct {
	calledSessionID string
	calledHint      string
	callCount       int
}

func (s *stubAgentLoop) InterruptSession(sessionID, hint string) error {
	s.calledSessionID = sessionID
	s.calledHint = hint
	s.callCount++
	return nil
}

func (s *stubAgentLoop) InterruptByChannelChat(channel, chatID, hint string) error {
	return nil
}

// TestCancelHandler_CallsInterruptSession verifies that the /cancel handler
// invokes InterruptSession on the agent loop with the correct session ID and a
// hint that contains the canceller identity (spec FR-27, FR-1).
func TestCancelHandler_CallsInterruptSession(t *testing.T) {
	stub := &stubAgentLoop{}

	rt := &Runtime{
		SessionID: func() string { return "session-abc" },
	}
	rt = rt.WithAgentLoop(stub)

	cancelDef := cancelCommand()
	if cancelDef.Handler == nil {
		t.Fatal("cancel handler must not be nil")
	}

	var reply string
	err := cancelDef.Handler(context.Background(), Request{
		Channel:  "telegram",
		SenderID: "@alice",
		Text:     "/cancel",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	}, rt)
	if err != nil {
		t.Fatalf("cancel handler returned unexpected error: %v", err)
	}

	if stub.callCount != 1 {
		t.Fatalf("InterruptSession call count = %d, want 1", stub.callCount)
	}
	if stub.calledSessionID != "session-abc" {
		t.Errorf("InterruptSession sessionID = %q, want %q", stub.calledSessionID, "session-abc")
	}

	// The hint must contain the canceller identity (UserID and Channel).
	for _, want := range []string{"@alice", "telegram"} {
		found := false
		for i := 0; i+len(want) <= len(stub.calledHint); i++ {
			if stub.calledHint[i:i+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("InterruptSession hint %q must contain %q", stub.calledHint, want)
		}
	}

	if reply != "⏸ Cancelling..." {
		t.Errorf("reply = %q, want %q", reply, "⏸ Cancelling...")
	}
}

// TestCancelHandler_NilRuntimeRepliesUnavailable verifies that a nil Runtime
// causes the handler to reply with the standard unavailable message rather than
// panicking.
func TestCancelHandler_NilRuntimeRepliesUnavailable(t *testing.T) {
	cancelDef := cancelCommand()

	var reply string
	err := cancelDef.Handler(context.Background(), Request{
		Text: "/cancel",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != unavailableMsg {
		t.Errorf("reply = %q, want %q", reply, unavailableMsg)
	}
}

// TestCancelHandler_NilAgentLoopReturnsSuccess verifies that CancelActiveTurn
// with no agent loop wired is a safe no-op — the handler still replies
// "⏸ Cancelling..." so the user sees acknowledgement.
func TestCancelHandler_NilAgentLoopReturnsSuccess(t *testing.T) {
	rt := &Runtime{
		SessionID: func() string { return "some-session" },
		// agentLoop intentionally left nil
	}

	cancelDef := cancelCommand()

	var reply string
	err := cancelDef.Handler(context.Background(), Request{
		Channel:  "web",
		SenderID: "user_123",
		Text:     "/cancel",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	}, rt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "⏸ Cancelling..." {
		t.Errorf("reply = %q, want %q", reply, "⏸ Cancelling...")
	}
}
