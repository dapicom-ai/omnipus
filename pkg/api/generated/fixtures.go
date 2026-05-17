package generated

import "time"

// ── WebSocket frame fixtures (AsyncAPI) ─────────────────────────────────────

// ToolApprovalRequiredFrame — the Ava-chat bug type. args MUST be a non-nil map.

func FixtureToolApprovalRequiredFrame_Populated() ToolApprovalRequiredFrame {
	return ToolApprovalRequiredFrame{
		Type:        "tool_approval_required",
		ApprovalId:  "ap-550e8400-e29b-41d4-a716-446655440001",
		ToolCallId:  "tc-550e8400-e29b-41d4-a716-446655440002",
		ToolName:    "workspace.shell",
		Args:        map[string]any{"command": "ls -la", "working_dir": "/tmp"},
		AgentId:     "jim",
		SessionId:   "sess-550e8400-e29b-41d4-a716-446655440003",
		TurnId:      "turn-550e8400-e29b-41d4-a716-446655440004",
		ExpiresInMs: 30000,
	}
}

// FixtureToolApprovalRequiredFrame_ZeroValue — Go zero values.
// Expected behavior: should FAIL JSON schema validation because:
//   - type is "" (schema requires const "tool_approval_required")
//   - approval_id is "" (minLength: 1)
//   - args is nil (marshals to null, schema requires type: object)
//   - other minLength:1 fields are ""
func FixtureToolApprovalRequiredFrame_ZeroValue() ToolApprovalRequiredFrame {
	return ToolApprovalRequiredFrame{}
}

// FixtureToolApprovalRequiredFrame_NilArgs — the exact state that caused the Ava-chat crash.
// args is nil → marshals to "args":null → schema rejects because type: object, not nullable.
func FixtureToolApprovalRequiredFrame_NilArgs() ToolApprovalRequiredFrame {
	return ToolApprovalRequiredFrame{
		Type:        "tool_approval_required",
		ApprovalId:  "ap-1",
		ToolCallId:  "tc-1",
		ToolName:    "workspace.shell",
		Args:        nil, // THE BUG — must be caught by the contract test
		AgentId:     "jim",
		SessionId:   "sess-1",
		TurnId:      "turn-1",
		ExpiresInMs: 30000,
	}
}

// FixtureToolApprovalRequiredFrame_Edge — unicode tool name, empty args object (valid), large expires_in_ms.
func FixtureToolApprovalRequiredFrame_Edge() ToolApprovalRequiredFrame {
	return ToolApprovalRequiredFrame{
		Type:        "tool_approval_required",
		ApprovalId:  "ap-edge-" + repeatStr("x", 100),
		ToolCallId:  "tc-edge-1",
		ToolName:    "system.spawn_subagent",
		Args:        map[string]any{}, // empty object is valid — not null
		AgentId:     "ava-🐙",
		SessionId:   "sess-edge-1",
		TurnId:      "turn-edge-1",
		ExpiresInMs: 86400000, // 24 hours
	}
}

// SessionStateFrame — pending_approvals MUST be a non-nil slice.

func FixtureSessionStateFrame_Populated() SessionStateFrame {
	return SessionStateFrame{
		Type:      "session_state",
		UserId:    "user-admin-1",
		EmittedAt: time.Now().UTC().Format(time.RFC3339),
		PendingApprovals: []SessionStatePendingApproval{
			{
				ApprovalId:  "ap-1",
				SessionId:   "sess-1",
				ToolName:    "workspace.shell",
				AgentId:     "jim",
				ExpiresInMs: 30000,
			},
		},
	}
}

// FixtureSessionStateFrame_ZeroValue — Go zero values.
// Expected: FAIL because type="", user_id="", pending_approvals=nil (marshals to null),
// emitted_at="" (not a valid date-time).
func FixtureSessionStateFrame_ZeroValue() SessionStateFrame {
	return SessionStateFrame{}
}

// FixtureSessionStateFrame_EmptyApprovals — valid: empty but non-nil slice.
// This is the common case when no approvals are pending.
func FixtureSessionStateFrame_EmptyApprovals() SessionStateFrame {
	return SessionStateFrame{
		Type:             "session_state",
		UserId:           "user-admin-1",
		EmittedAt:        "2026-05-17T10:00:00Z",
		PendingApprovals: []SessionStatePendingApproval{}, // empty slice, not nil
	}
}

// FixtureSessionStateFrame_Edge — multiple approvals, unicode user ID.
func FixtureSessionStateFrame_Edge() SessionStateFrame {
	return SessionStateFrame{
		Type:      "session_state",
		UserId:    "user-unicode-🔑",
		EmittedAt: "2026-05-17T00:00:01Z",
		PendingApprovals: []SessionStatePendingApproval{
			{ApprovalId: "ap-1", SessionId: "sess-1", ToolName: "tool.a", AgentId: "jim", ExpiresInMs: 1},
			{ApprovalId: "ap-2", SessionId: "sess-2", ToolName: "tool.b", AgentId: "ava", ExpiresInMs: 2},
			{ApprovalId: "ap-3", SessionId: "sess-3", ToolName: "tool.c", AgentId: "rex", ExpiresInMs: 3},
		},
	}
}

// MediaFrame — parts MUST be a non-nil, non-empty slice.

func FixtureMediaFrame_Populated() MediaFrame {
	return MediaFrame{
		Type:      "media",
		SessionId: "sess-1",
		Parts: []MediaPart{
			{
				Type:        "image",
				Url:         "/api/v1/media/screenshot-abc123.png",
				Filename:    "screenshot.png",
				ContentType: "image/png",
				Caption:     strPtr("Browser screenshot"),
			},
		},
	}
}

// FixtureMediaFrame_ZeroValue — Go zero values.
// Expected: FAIL because type="", session_id="", parts=nil (marshals to null).
func FixtureMediaFrame_ZeroValue() MediaFrame {
	return MediaFrame{}
}

// FixtureMediaFrame_NilParts — parts is nil — this must FAIL validation.
func FixtureMediaFrame_NilParts() MediaFrame {
	return MediaFrame{
		Type:      "media",
		SessionId: "sess-1",
		Parts:     nil, // must fail: schema requires array with minItems: 1
	}
}

// FixtureMediaFrame_Edge — multiple parts with various filenames.
func FixtureMediaFrame_Edge() MediaFrame {
	return MediaFrame{
		Type:      "media",
		SessionId: "sess-edge-1",
		Parts: []MediaPart{
			{Type: "image", Url: "/api/v1/media/img1.png", Filename: "screenshot.png", ContentType: "image/png"},
			{Type: "file", Url: "/api/v1/media/doc1.pdf", Filename: "report_2026.pdf", ContentType: "application/pdf"},
			{
				Type:        "audio",
				Url:         "/api/v1/media/clip1.mp3",
				Filename:    "recording.mp3",
				ContentType: "audio/mpeg",
				Caption:     strPtr("Voice note"),
			},
		},
	}
}

// ToolCallStartFrame — params MUST be a non-nil map.

func FixtureToolCallStartFrame_Populated() ToolCallStartFrame {
	parentCallId := "parent-call-abc"
	agentId := "jim"
	return ToolCallStartFrame{
		Type:         "tool_call_start",
		SessionId:    "sess-1",
		Tool:         "workspace.shell",
		CallId:       "call-xyz-1",
		Params:       map[string]any{"command": "echo hello", "working_dir": "/workspace"},
		ParentCallId: &parentCallId,
		AgentId:      &agentId,
	}
}

// FixtureToolCallStartFrame_ZeroValue — Go zero values.
// Expected: FAIL because type="", session_id="", tool="", call_id="", params=nil.
func FixtureToolCallStartFrame_ZeroValue() ToolCallStartFrame {
	return ToolCallStartFrame{}
}

// FixtureToolCallStartFrame_NilParams — params is nil — this must FAIL validation.
func FixtureToolCallStartFrame_NilParams() ToolCallStartFrame {
	return ToolCallStartFrame{
		Type:      "tool_call_start",
		SessionId: "sess-1",
		Tool:      "workspace.shell",
		CallId:    "call-1",
		Params:    nil, // must fail: schema requires type: object
	}
}

// FixtureToolCallStartFrame_Edge — no-parameter tool, no parent call.
func FixtureToolCallStartFrame_Edge() ToolCallStartFrame {
	return ToolCallStartFrame{
		Type:      "tool_call_start",
		SessionId: "sess-edge-1",
		Tool:      "system.ping",
		CallId:    "call-edge-" + repeatStr("a", 50),
		Params:    map[string]any{}, // valid empty object (no-arg tool)
	}
}

// DoneFrame

func FixtureDoneFrame_Populated() DoneFrame {
	tokens := float64(1234)
	cost := float64(0.00412)
	durationMs := float64(3720)
	tokensDropped := float64(2)
	framesEmitted := float64(47)
	orphanCount := float64(0)
	dupCount := float64(0)
	truncCount := float64(1)
	replayErr := false
	return DoneFrame{
		Type:      "done",
		SessionId: "sess-1",
		Stats: &DoneStats{
			Tokens:                   &tokens,
			Cost:                     &cost,
			DurationMs:               &durationMs,
			TokensDropped:            &tokensDropped,
			FramesEmitted:            &framesEmitted,
			OrphanCount:              &orphanCount,
			DuplicateToolCallIdCount: &dupCount,
			TruncatedResultCount:     &truncCount,
			ReplayError:              &replayErr,
		},
	}
}

func FixtureDoneFrame_ZeroValue() DoneFrame {
	return DoneFrame{}
}

func FixtureDoneFrame_NoStats() DoneFrame {
	return DoneFrame{
		Type:      "done",
		SessionId: "sess-1",
		Stats:     nil, // stats is optional per schema
	}
}

func FixtureDoneFrame_Edge() DoneFrame {
	cost := float64(0)
	return DoneFrame{
		Type:      "done",
		SessionId: "sess-unicode-🏁",
		Stats:     &DoneStats{Cost: &cost},
	}
}

// ErrorFrame

func FixtureErrorFrame_Populated() ErrorFrame {
	sessId := "sess-1"
	return ErrorFrame{
		Type:      "error",
		Message:   "LLM rate limit exceeded — retry after 60 seconds",
		SessionId: &sessId,
	}
}

func FixtureErrorFrame_ZeroValue() ErrorFrame {
	return ErrorFrame{}
}

func FixtureErrorFrame_Edge() ErrorFrame {
	return ErrorFrame{
		Type:    "error",
		Message: repeatStr("x", 4096), // long error message
	}
}

// TokenFrame

func FixtureTokenFrame_Populated() TokenFrame {
	return TokenFrame{
		Type:      "token",
		SessionId: "sess-1",
		Content:   "Hello, world!",
	}
}

func FixtureTokenFrame_ZeroValue() TokenFrame {
	return TokenFrame{}
}

func FixtureTokenFrame_Edge() TokenFrame {
	return TokenFrame{
		Type:      "token",
		SessionId: "sess-edge-1",
		Content:   "streaming token with special chars: -- hello world 123",
	}
}

// ToolCallResultFrame

func FixtureToolCallResultFrame_Populated() ToolCallResultFrame {
	durationMs := 128
	agentId := "jim"
	parentCallId := "parent-1"
	return ToolCallResultFrame{
		Type:         "tool_call_result",
		SessionId:    "sess-1",
		Tool:         "workspace.shell",
		CallId:       "call-1",
		Result:       map[string]any{"stdout": "hello\n", "exit_code": float64(0)},
		Status:       "success",
		DurationMs:   &durationMs,
		AgentId:      &agentId,
		ParentCallId: &parentCallId,
	}
}

func FixtureToolCallResultFrame_ZeroValue() ToolCallResultFrame {
	return ToolCallResultFrame{}
}

func FixtureToolCallResultFrame_Error() ToolCallResultFrame {
	errMsg := "command not found: foobar"
	return ToolCallResultFrame{
		Type:      "tool_call_result",
		SessionId: "sess-1",
		Tool:      "workspace.shell",
		CallId:    "call-err-1",
		Result:    nil,
		Status:    "error",
		Error:     &errMsg,
	}
}

func FixtureToolCallResultFrame_Edge() ToolCallResultFrame {
	return ToolCallResultFrame{
		Type:      "tool_call_result",
		SessionId: "sess-edge-1",
		Tool:      "system.spawn_subagent",
		CallId:    "call-edge-1",
		Result:    "plain string result", // result is oneOf: any value is valid
		Status:    "success",
	}
}

// SessionStartedFrame

func FixtureSessionStartedFrame_Populated() SessionStartedFrame {
	agentId := "jim"
	return SessionStartedFrame{
		Type:      "session_started",
		SessionId: "sess-new-1",
		AgentId:   &agentId,
	}
}

func FixtureSessionStartedFrame_ZeroValue() SessionStartedFrame {
	return SessionStartedFrame{}
}

// SubagentStartFrame

func FixtureSubagentStartFrame_Populated() SubagentStartFrame {
	agentId := "ava"
	return SubagentStartFrame{
		Type:         "subagent_start",
		SessionId:    "sess-1",
		SpanId:       "span-abc-123",
		ParentCallId: "parent-call-1",
		TaskLabel:    "Research latest AI papers",
		AgentId:      &agentId,
	}
}

func FixtureSubagentStartFrame_ZeroValue() SubagentStartFrame {
	return SubagentStartFrame{}
}

// SubagentEndFrame

func FixtureSubagentEndFrame_Populated() SubagentEndFrame {
	agentId := "ava"
	durationMs := 4500
	finalResult := "Found 3 relevant papers"
	msg := "Subagent completed successfully"
	parentCallId := "parent-call-1"
	reason := "parent_done_early"
	return SubagentEndFrame{
		Type:         "subagent_end",
		SessionId:    "sess-1",
		SpanId:       "span-abc-123",
		Status:       "success",
		AgentId:      &agentId,
		DurationMs:   &durationMs,
		FinalResult:  &finalResult,
		Message:      &msg,
		ParentCallId: &parentCallId,
		Reason:       &reason,
	}
}

func FixtureSubagentEndFrame_ZeroValue() SubagentEndFrame {
	return SubagentEndFrame{}
}

// ExecApprovalRequestFrame

func FixtureExecApprovalRequestFrame_Populated() ExecApprovalRequestFrame {
	matchedPolicy := "allow_list"
	workingDir := "/workspace"
	return ExecApprovalRequestFrame{
		Type:          "exec_approval_request",
		SessionId:     "sess-1",
		Id:            "exec-req-1",
		Command:       "rm -rf /tmp/build",
		MatchedPolicy: &matchedPolicy,
		WorkingDir:    &workingDir,
	}
}

func FixtureExecApprovalRequestFrame_ZeroValue() ExecApprovalRequestFrame {
	return ExecApprovalRequestFrame{}
}

// ReplayMessageFrame

func FixtureReplayMessageFrame_Populated() ReplayMessageFrame {
	agentId := "jim"
	msgId := "msg-uuid-1"
	ts := "2026-05-17T10:00:00Z"
	return ReplayMessageFrame{
		Type:      "replay_message",
		SessionId: "sess-1",
		Role:      "assistant",
		Content:   "Hello! How can I help you today?",
		AgentId:   &agentId,
		Id:        &msgId,
		Timestamp: &ts,
	}
}

func FixtureReplayMessageFrame_ZeroValue() ReplayMessageFrame {
	return ReplayMessageFrame{}
}

// RateLimitFrame

func FixtureRateLimitFrame_Populated() RateLimitFrame {
	agentId := "jim"
	tool := "workspace.shell"
	return RateLimitFrame{
		Type:              "rate_limit",
		SessionId:         "sess-1",
		PolicyRule:        "100req/min",
		Resource:          "workspace.shell",
		RetryAfterSeconds: 60.0,
		Scope:             "agent",
		AgentId:           &agentId,
		Tool:              &tool,
	}
}

func FixtureRateLimitFrame_ZeroValue() RateLimitFrame {
	return RateLimitFrame{}
}

// AgentSwitchedFrame

func FixtureAgentSwitchedFrame_Populated() AgentSwitchedFrame {
	agentId := "ava"
	msg := "Switched to Ava for research task"
	return AgentSwitchedFrame{
		Type:      "agent_switched",
		SessionId: "sess-1",
		AgentId:   &agentId,
		Message:   &msg,
	}
}

func FixtureAgentSwitchedFrame_ZeroValue() AgentSwitchedFrame {
	return AgentSwitchedFrame{}
}

// TaskStatusChangedFrame

func FixtureTaskStatusChangedFrame_Populated() TaskStatusChangedFrame {
	agentId := "jim"
	return TaskStatusChangedFrame{
		Type:      "task_status_changed",
		SessionId: "sess-1",
		TaskId:    "task-uuid-1",
		Status:    "completed",
		AgentId:   &agentId,
	}
}

func FixtureTaskStatusChangedFrame_ZeroValue() TaskStatusChangedFrame {
	return TaskStatusChangedFrame{}
}

// SystemOverloadFrame

func FixtureSystemOverloadFrame_Populated() SystemOverloadFrame {
	msg := "System at capacity — please retry in 30 seconds"
	return SystemOverloadFrame{
		Type:      "system_overload",
		SessionId: "sess-1",
		Message:   &msg,
	}
}

func FixtureSystemOverloadFrame_ZeroValue() SystemOverloadFrame {
	return SystemOverloadFrame{}
}

// CancelStageFrame

func FixtureCancelStageFrame_Populated() CancelStageFrame {
	return CancelStageFrame{
		Type:      "cancel_stage",
		SessionId: "sess-1",
		Stage:     "graceful",
	}
}

func FixtureCancelStageFrame_ZeroValue() CancelStageFrame {
	return CancelStageFrame{}
}

// ReplayWarningFrame

func FixtureReplayWarningFrame_Populated() ReplayWarningFrame {
	dupCount := 3
	return ReplayWarningFrame{
		Type:      "replay_warning",
		SessionId: "sess-1",
		Message:   "Duplicate tool_call_ids detected during replay",
		Stats:     &ReplayWarningStats{DuplicateToolCallIdCount: &dupCount},
	}
}

func FixtureReplayWarningFrame_ZeroValue() ReplayWarningFrame {
	return ReplayWarningFrame{}
}

// SessionCloseAckFrame

func FixtureSessionCloseAckFrame_Populated() SessionCloseAckFrame {
	id := "close-ack-1"
	return SessionCloseAckFrame{
		Type:      "session_close_ack",
		SessionId: "sess-1",
		Id:        &id,
	}
}

func FixtureSessionCloseAckFrame_ZeroValue() SessionCloseAckFrame {
	return SessionCloseAckFrame{}
}

// DevicePairingRequestFrame

func FixtureDevicePairingRequestFrame_Populated() DevicePairingRequestFrame {
	name := "iPhone 15"
	fp := "SHA256:abc123"
	code := "XK7P-9QR2"
	sessId := "sess-1"
	return DevicePairingRequestFrame{
		Type:        "device_pairing_request",
		DeviceId:    "device-uuid-1",
		DeviceName:  &name,
		Fingerprint: &fp,
		PairingCode: &code,
		SessionId:   &sessId,
	}
}

func FixtureDevicePairingRequestFrame_ZeroValue() DevicePairingRequestFrame {
	return DevicePairingRequestFrame{}
}

// ExecApprovalResponseAckFrame

func FixtureExecApprovalResponseAckFrame_Populated() ExecApprovalResponseAckFrame {
	id := "exec-req-1"
	sessId := "sess-1"
	return ExecApprovalResponseAckFrame{
		Type:      "exec_approval_response_ack",
		Id:        &id,
		SessionId: &sessId,
	}
}

func FixtureExecApprovalResponseAckFrame_ZeroValue() ExecApprovalResponseAckFrame {
	return ExecApprovalResponseAckFrame{}
}

// ── REST response type fixtures (OpenAPI) ────────────────────────────────────

// LoginResponse

func FixtureLoginResponse_Populated() LoginResponse {
	warning := strPtr("API key stored in plaintext")
	return LoginResponse{
		Token:    "omnipus_" + repeatStr("a", 64),
		Role:     "admin",
		Username: "admin",
		Warning:  warning,
	}
}

func FixtureLoginResponse_ZeroValue() LoginResponse {
	return LoginResponse{}
}

func FixtureLoginResponse_Edge() LoginResponse {
	return LoginResponse{
		Token:    "omnipus_" + repeatStr("f", 64),
		Role:     "user",
		Username: "unicode-user-🔑",
	}
}

// Session

func FixtureSession_Populated() Session {
	agentId := "jim"
	activeAgentId := "jim"
	model := "claude-sonnet-4-6"
	sessionType := SessionType("chat")
	partitions := []string{"2026-05-16.jsonl", "2026-05-17.jsonl"}
	return Session{
		Id:            "550e8400-e29b-41d4-a716-446655440000",
		AgentId:       "jim",
		ActiveAgentId: &activeAgentId,
		AgentIds:      &[]string{agentId},
		Title:         "My test session",
		Status:        "active",
		CreatedAt:     time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC),
		Channel:       "webchat",
		Partitions:    partitions,
		Model:         &model,
		Type:          &sessionType,
		Stats: struct {
			Cost         float64 `json:"cost"`
			MessageCount int     `json:"message_count"`
			TokensIn     int     `json:"tokens_in"`
			TokensOut    int     `json:"tokens_out"`
			TokensTotal  int     `json:"tokens_total"`
			ToolCalls    int     `json:"tool_calls"`
		}{
			Cost:         0.0412,
			MessageCount: 10,
			TokensIn:     1200,
			TokensOut:    800,
			TokensTotal:  2000,
			ToolCalls:    3,
		},
	}
}

// FixtureSession_ZeroValue — Go zero values.
// Expected: FAIL because required fields (id, agent_id, title, status, etc.) are "".
// Also: created_at/updated_at are time.Time{} which marshals to "0001-01-01T00:00:00Z"
// (technically valid RFC3339 but wrong per the "reasonable year" assertion).
func FixtureSession_ZeroValue() Session {
	return Session{}
}

func FixtureSession_Edge() Session {
	sessionType := SessionType("task")
	return Session{
		Id:         "00000000-0000-0000-0000-000000000001",
		AgentId:    "custom-agent-" + repeatStr("x", 36),
		Title:      "Edge case session title with special chars",
		Status:     "archived",
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
		Channel:    "telegram",
		Partitions: []string{},
		Type:       &sessionType,
		Stats: struct {
			Cost         float64 `json:"cost"`
			MessageCount int     `json:"message_count"`
			TokensIn     int     `json:"tokens_in"`
			TokensOut    int     `json:"tokens_out"`
			TokensTotal  int     `json:"tokens_total"`
			ToolCalls    int     `json:"tool_calls"`
		}{},
	}
}

// Agent

func FixtureAgent_Populated() Agent {
	color := "#D4AF37"
	icon := "Robot"
	model := "claude-sonnet-4-6"
	sandboxProfile := AgentSandboxProfile("workspace")
	warning := strPtr("Config reload failed after update")
	return Agent{
		Id:                "jim",
		Name:              "Jim",
		Type:              AgentTypeCore,
		Locked:            true,
		Status:            AgentStatusIdle,
		Soul:              "You are Jim, a helpful assistant.",
		Heartbeat:         "",
		Instructions:      "",
		TimeoutSeconds:    300,
		MaxToolIterations: 50,
		SteeringMode:      "one-at-a-time",
		ToolFeedback:      true,
		HeartbeatEnabled:  false,
		HeartbeatInterval: 300,
		Color:             &color,
		Icon:              &icon,
		Model:             &model,
		SandboxProfile:    &sandboxProfile,
		Warning:           warning,
	}
}

func FixtureAgent_ZeroValue() Agent {
	return Agent{}
}

func FixtureAgent_Edge() Agent {
	return Agent{
		Id:                "custom-" + repeatStr("y", 36),
		Name:              "Unicode Agent 🤖",
		Type:              AgentTypeCustom,
		Locked:            false,
		Status:            AgentStatusDraft,
		Soul:              "",
		Heartbeat:         "",
		Instructions:      "# Instructions\n\nBe helpful.\n",
		TimeoutSeconds:    0,
		MaxToolIterations: 0,
		SteeringMode:      "one-at-a-time",
		ToolFeedback:      false,
		HeartbeatEnabled:  false,
		HeartbeatInterval: 0,
	}
}

// LoginResponse — User response

func FixtureUser_Populated() User {
	return User{
		Username:       "alice",
		Role:           "admin",
		HasPassword:    true,
		HasActiveToken: true,
	}
}

func FixtureUser_ZeroValue() User {
	return User{}
}

func FixtureUser_Edge() User {
	return User{
		Username:       "bob-" + repeatStr("z", 55), // long but valid
		Role:           "user",
		HasPassword:    false,
		HasActiveToken: false,
	}
}

// HealthResponse

func FixtureHealthResponse_Populated() HealthResponse {
	return HealthResponse{
		Status: "ok",
	}
}

func FixtureHealthResponse_ZeroValue() HealthResponse {
	return HealthResponse{}
}

// ── helper functions ─────────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }

func repeatStr(s string, n int) string {
	result := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		result = append(result, s...)
	}
	return string(result)
}
