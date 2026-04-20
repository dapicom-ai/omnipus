//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/dapicom-ai/omnipus/pkg/session"
)

// replayMaxResultBytes is the maximum JSON-encoded size of a tool_call_result
// frame's result field before it is truncated. Per FR-I-011: 1 MiB.
const replayMaxResultBytes = 1 * 1024 * 1024

// replayResultPreviewBytes is the number of bytes preserved in the preview
// when a result is truncated. Per FR-I-011: 10 KiB.
const replayResultPreviewBytes = 10 * 1024

// streamReplay emits replay frames for the given transcript entries, calling
// emit for each frame in order.  It is extracted from handleAttachSession so
// that unit tests can drive it with a slice-backed sink without a real
// WebSocket connection.
//
// Contract (per Sprint I spec):
//   - Compaction entries are skipped (FR-I-006).
//   - For user/system entries: emit replay_message{role, content, agent_id}.
//   - For assistant entries: emit replay_message if content is non-empty, then
//     for each ToolCall emit tool_call_start + tool_call_result (FR-I-001).
//   - Spawn spans: scan all entries first to build the set of spawn IDs whose
//     children are present.  Nested tool calls (ParentToolCallID != "") are
//     wrapped with subagent_start / subagent_end when the parent is in the set
//     (FR-I-003).  Orphan parents log slog.Warn (FR-I-007).
//   - Duplicate ToolCall.IDs: only the last occurrence is emitted; earlier ones
//     log slog.Warn (FR-I-012).
//   - Oversized results are truncated (FR-I-011).
//   - Context cancellation is honoured between every frame (FR-I-005).
//   - Returns after emitting exactly one done frame (FR-I-004).
//
// The returned error is non-nil only when emit itself returns an error (e.g.
// context cancelled or send-channel full).
func streamReplay(
	ctx context.Context,
	sessionID string,
	entries []session.TranscriptEntry,
	emit func(wsServerFrame) error,
) (framesEmitted int, err error) {
	// ── Pass 1: build ancillary indexes ─────────────────────────────────────

	// spawnIDsPresent: set of ToolCall.IDs where tool == "spawn" AND at least
	// one other tool call in the transcript has ParentToolCallID == that ID.
	// This is the signal that the parent span has live children to bracket.
	spawnIDsWithChildren := buildSpawnIDsWithChildren(entries)

	// deduped: for each ToolCall.ID keep only the index of the last occurrence
	// across ALL entries.  key = ToolCall.ID, value = (entryIdx, tcIdx).
	latestByID := make(map[string]tcAddr)
	for ei, entry := range entries {
		for ti, tc := range entry.ToolCalls {
			if tc.ID == "" {
				continue
			}
			if prev, dup := latestByID[tc.ID]; dup {
				// Warn only once (on the first detected duplicate). Subsequent
				// occurrences silently overwrite — the last one wins.
				_ = prev
				slog.Warn("replay: duplicate tool_call_id detected — only latest will emit",
					"event", "replay_duplicate_tool_call_id",
					"session_id", sessionID,
					"tool_call_id", tc.ID,
				)
			}
			latestByID[tc.ID] = tcAddr{ei, ti}
		}
	}

	// ── Pass 2: emit frames ──────────────────────────────────────────────────

	emitFrame := func(f wsServerFrame) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err2 := emit(f); err2 != nil {
			return err2
		}
		framesEmitted++
		return nil
	}

	for ei, entry := range entries {
		// FR-I-006: skip compaction entries.
		if entry.Type == session.EntryTypeCompaction {
			continue
		}

		// FR-I-002: emit replay_message for non-empty content.
		if entry.Content != "" {
			f := wsServerFrame{
				Type:    "replay_message",
				Role:    entry.Role,
				Content: entry.Content,
			}
			if entry.AgentID != "" {
				f.AgentID = entry.AgentID
			}
			if err2 := emitFrame(f); err2 != nil {
				return framesEmitted, err2
			}
		}

		// FR-I-001: emit tool_call_start + tool_call_result for each ToolCall.
		for ti, tc := range entry.ToolCalls {
			if tc.ID == "" {
				continue
			}
			// Dedup: skip if this is not the latest occurrence.
			if latest := latestByID[tc.ID]; latest.entryIdx != ei || latest.tcIdx != ti {
				continue
			}

			isNested := tc.ParentToolCallID != ""
			parentIsSpawn := isNested && spawnIDsWithChildren[tc.ParentToolCallID]

			if isNested && parentIsSpawn {
				// This tool call will be emitted by emitNestedToolCalls when its
				// parent spawn is processed.  Skip it here to avoid double-emission.
				continue
			}

			if isNested && !parentIsSpawn {
				// FR-I-007: orphan — parent not found in transcript.
				slog.Warn("replay: orphan tool call — parent spawn not in transcript",
					"event", "replay_orphan",
					"session_id", sessionID,
					"parent_tool_call_id", tc.ParentToolCallID,
				)
			}

			// For spawn calls that have children: emit subagent_start before
			// the nested frames, then subagent_end after.  We detect this
			// entry as a spawn-parent if its own ID is in spawnIDsWithChildren.
			isSpawnParent := spawnIDsWithChildren[tc.ID]

			if isSpawnParent {
				// Emit tool_call_start for the spawn call itself FIRST.
				startFrame := wsServerFrame{
					Type:   "tool_call_start",
					CallID: tc.ID,
					Tool:   tc.Tool,
					Params: tc.Parameters,
				}
				if entry.AgentID != "" {
					startFrame.AgentID = entry.AgentID
				}
				if err2 := emitFrame(startFrame); err2 != nil {
					return framesEmitted, err2
				}

				// Emit subagent_start to bracket nested frames.
				spanID := "span_" + tc.ID
				taskLabel := resolveTaskLabel(tc)
				subStart := wsServerFrame{
					Type:         "subagent_start",
					SpanID:       spanID,
					ParentCallID: tc.ID,
					TaskLabel:    taskLabel,
				}
				if entry.AgentID != "" {
					subStart.AgentID = entry.AgentID
				}
				if err2 := emitFrame(subStart); err2 != nil {
					return framesEmitted, err2
				}

				// Emit all nested tool calls (children with ParentToolCallID == tc.ID).
				nestedDurationMS, nestedStatus, nestedErr := emitNestedToolCalls(
					ctx, sessionID, tc.ID, entries, latestByID, entry.AgentID, emitFrame,
				)
				if nestedErr != nil {
					return framesEmitted, nestedErr
				}

				// Emit subagent_end.
				subEnd := wsServerFrame{
					Type:       "subagent_end",
					SpanID:     spanID,
					DurationMs: nestedDurationMS,
					Status:     nestedStatus,
				}
				if entry.AgentID != "" {
					subEnd.AgentID = entry.AgentID
				}
				if err2 := emitFrame(subEnd); err2 != nil {
					return framesEmitted, err2
				}

				// Emit tool_call_result for the spawn call.
				resultPayload := truncateResult(sessionID, tc)
				resultFrame := wsServerFrame{
					Type:       "tool_call_result",
					CallID:     tc.ID,
					Tool:       tc.Tool,
					Result:     resultPayload,
					Status:     resolveStatus(tc.Status),
					DurationMs: tc.DurationMS,
				}
				if entry.AgentID != "" {
					resultFrame.AgentID = entry.AgentID
				}
				if err2 := emitFrame(resultFrame); err2 != nil {
					return framesEmitted, err2
				}
				continue
			}

			// Regular (non-spawn, or nested) tool call: flat emission.
			startFrame := wsServerFrame{
				Type:   "tool_call_start",
				CallID: tc.ID,
				Tool:   tc.Tool,
				Params: tc.Parameters,
			}
			if entry.AgentID != "" {
				startFrame.AgentID = entry.AgentID
			}
			if isNested {
				startFrame.ParentCallID = tc.ParentToolCallID
			}
			if err2 := emitFrame(startFrame); err2 != nil {
				return framesEmitted, err2
			}

			resultPayload := truncateResult(sessionID, tc)
			resultFrame := wsServerFrame{
				Type:       "tool_call_result",
				CallID:     tc.ID,
				Tool:       tc.Tool,
				Result:     resultPayload,
				Status:     resolveStatus(tc.Status),
				DurationMs: tc.DurationMS,
			}
			if entry.AgentID != "" {
				resultFrame.AgentID = entry.AgentID
			}
			if isNested {
				resultFrame.ParentCallID = tc.ParentToolCallID
			}
			if err2 := emitFrame(resultFrame); err2 != nil {
				return framesEmitted, err2
			}
		}
	}

	// FR-I-004: exactly one done frame at the end.
	if err2 := emitFrame(wsServerFrame{Type: "done", Stats: map[string]any{}}); err2 != nil {
		return framesEmitted, err2
	}
	return framesEmitted, nil
}

// buildSpawnIDsWithChildren scans all entries and returns the set of spawn
// tool call IDs that have at least one child (another tool call carrying that
// ID as ParentToolCallID).  This is used to determine whether to bracket a
// spawn with subagent_start / subagent_end.
func buildSpawnIDsWithChildren(entries []session.TranscriptEntry) map[string]bool {
	// Collect all spawn IDs.
	spawnIDs := make(map[string]bool)
	for _, entry := range entries {
		for _, tc := range entry.ToolCalls {
			if tc.Tool == "spawn" && tc.ID != "" {
				spawnIDs[tc.ID] = false // not yet confirmed to have children
			}
		}
	}
	// Mark those that have at least one child.
	for _, entry := range entries {
		for _, tc := range entry.ToolCalls {
			if tc.ParentToolCallID != "" {
				if _, ok := spawnIDs[tc.ParentToolCallID]; ok {
					spawnIDs[tc.ParentToolCallID] = true
				}
			}
		}
	}
	// Keep only confirmed spawns with children.
	result := make(map[string]bool)
	for id, hasChildren := range spawnIDs {
		if hasChildren {
			result[id] = true
		}
	}
	return result
}

type tcAddr struct{ entryIdx, tcIdx int }

// emitNestedToolCalls emits all tool calls across all entries whose
// ParentToolCallID == parentID.  It respects dedup (latestByID), emits
// start+result pairs, and returns the aggregate duration and status.
func emitNestedToolCalls(
	ctx context.Context,
	sessionID string,
	parentID string,
	entries []session.TranscriptEntry,
	latestByID map[string]tcAddr,
	agentID string,
	emitFrame func(wsServerFrame) error,
) (totalDurationMS int64, aggregateStatus string, err error) {
	aggregateStatus = "success"

	for ei, entry := range entries {
		for ti, tc := range entry.ToolCalls {
			if tc.ParentToolCallID != parentID {
				continue
			}
			if tc.ID == "" {
				continue
			}
			// Dedup.
			if latest := latestByID[tc.ID]; latest.entryIdx != ei || latest.tcIdx != ti {
				continue
			}

			if ctx.Err() != nil {
				return totalDurationMS, aggregateStatus, ctx.Err()
			}

			startFrame := wsServerFrame{
				Type:         "tool_call_start",
				CallID:       tc.ID,
				Tool:         tc.Tool,
				Params:       tc.Parameters,
				ParentCallID: parentID,
			}
			effectiveAgentID := entry.AgentID
			if effectiveAgentID == "" {
				effectiveAgentID = agentID
			}
			if effectiveAgentID != "" {
				startFrame.AgentID = effectiveAgentID
			}
			if err2 := emitFrame(startFrame); err2 != nil {
				return totalDurationMS, aggregateStatus, err2
			}

			resultPayload := truncateResult(sessionID, tc)
			status := resolveStatus(tc.Status)
			resultFrame := wsServerFrame{
				Type:         "tool_call_result",
				CallID:       tc.ID,
				Tool:         tc.Tool,
				Result:       resultPayload,
				Status:       status,
				DurationMs:   tc.DurationMS,
				ParentCallID: parentID,
			}
			if effectiveAgentID != "" {
				resultFrame.AgentID = effectiveAgentID
			}
			if err2 := emitFrame(resultFrame); err2 != nil {
				return totalDurationMS, aggregateStatus, err2
			}

			totalDurationMS += tc.DurationMS
			if status == "error" {
				aggregateStatus = "error"
			}
		}
	}
	return totalDurationMS, aggregateStatus, nil
}

// truncateResult JSON-encodes tc.Result and, if it exceeds replayMaxResultBytes,
// replaces it with a truncation marker per FR-I-011.
// Returns the value to place in wsServerFrame.Result.
func truncateResult(sessionID string, tc session.ToolCall) any {
	if tc.Result == nil {
		return tc.Result
	}
	encoded, err := json.Marshal(tc.Result)
	if err != nil {
		// Marshal failure: return a sentinel map so the downstream WS encoder always
		// succeeds. Passing the raw value through would cause an identical failure at
		// the next marshal site, silently corrupting the replay frame.
		slog.Error("replay: tool_call_result marshal failed — emitting sentinel",
			"event", "replay_result_marshal_error",
			"session_id", sessionID,
			"tool_call_id", tc.ID,
			"error", err,
		)
		return map[string]any{"_marshal_error": err.Error()}
	}
	if len(encoded) <= replayMaxResultBytes {
		return tc.Result
	}
	// Truncate: emit marker with preview.
	originalSize := len(encoded)
	preview := encoded
	if len(preview) > replayResultPreviewBytes {
		preview = encoded[:replayResultPreviewBytes]
	}
	slog.Warn("replay: tool_call_result exceeds 1 MiB — truncating",
		"event", "replay_result_truncated",
		"session_id", sessionID,
		"tool_call_id", tc.ID,
		"original_size_bytes", originalSize,
	)
	return map[string]any{
		"_truncated":          true,
		"original_size_bytes": originalSize,
		"preview":             string(preview),
	}
}

// resolveStatus normalises an empty status string to "success".
func resolveStatus(s string) string {
	if s == "" {
		return "success"
	}
	return s
}

// resolveTaskLabel extracts the task label from a spawn tool call's parameters.
// Prefers Parameters["label"]; falls back to Parameters["task"] truncated at 60 chars.
func resolveTaskLabel(tc session.ToolCall) string {
	if tc.Parameters == nil {
		return ""
	}
	if label, ok := tc.Parameters["label"].(string); ok && label != "" {
		return label
	}
	if task, ok := tc.Parameters["task"].(string); ok {
		runes := []rune(task)
		if len(runes) > 60 {
			return string(runes[:60])
		}
		return task
	}
	return ""
}

// replayStats aggregates metrics from a set of transcript entries for slog.Info.
type replayStats struct {
	toolCallCount  int
	spanCount      int
}

// computeReplayStats scans entries for logging purposes.
func computeReplayStats(entries []session.TranscriptEntry) replayStats {
	var rs replayStats
	spawnIDsWithChildren := buildSpawnIDsWithChildren(entries)
	for _, entry := range entries {
		rs.toolCallCount += len(entry.ToolCalls)
	}
	rs.spanCount = len(spawnIDsWithChildren)
	return rs
}

// wsEmitFunc returns an emit function that writes frames to a wsConn's sendCh,
// respecting context cancellation.
func wsEmitFunc(ctx context.Context, wc *wsConn) func(wsServerFrame) error {
	return func(f wsServerFrame) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		data, err := json.Marshal(f)
		if err != nil {
			return err
		}
		select {
		case wc.sendCh <- data:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

