// AssistantUI runtime adapter — bridges the Zustand chat store + WebSocket protocol
// to useExternalStoreRuntime so AssistantUI primitives can render our messages.

import { useExternalStoreRuntime } from "@assistant-ui/react";
import type { ThreadMessageLike, AppendMessage } from "@assistant-ui/react";
import { useChatStore } from "@/store/chat";
import type { ChatMessage } from "@/store/chat";
import type { ToolCall } from "@/lib/api";
import { useUiStore } from "@/store/ui";

type StoreToolCall = ToolCall & { call_id: string };

// ── Message conversion ────────────────────────────────────────────────────────

/** Push text + resolved history tool calls onto parts (text first, then tool calls). */
function pushHistoryParts(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  parts: any[],
  text: string,
  historyToolCalls: NonNullable<ChatMessage["tool_calls"]>,
  toolCalls: Record<string, StoreToolCall>,
): void {
  parts.push({ type: "text", text });
  for (const tc of historyToolCalls) {
    const resolved: ToolCall = toolCalls[tc.id] ?? tc;
    parts.push({
      type: "tool-call",
      toolCallId: tc.id,
      toolName: tc.tool,
      args: tc.params,
      result: resolved.result,
    });
  }
}

function buildContentParts(
  msg: ChatMessage,
  toolCalls: Record<string, StoreToolCall>,
  toolCallOrder: string[],
  textAtToolCallStart: Record<string, string>,
  isLastAssistant: boolean
): ThreadMessageLike["content"] {
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const parts = [] as any;
    const historyTCs = msg.tool_calls ?? [];

    // Non-last or non-assistant messages: text first, then tool calls (no interleaving).
    if (!isLastAssistant || msg.role !== "assistant") {
      pushHistoryParts(parts, msg.content, historyTCs, toolCalls);
      return parts;
    }

    // Last assistant message: check for live (in-progress) tool calls to interleave.
    const seenIds = new Set(historyTCs.map((tc) => tc.id));
    const liveIds = toolCallOrder.filter((id) => !seenIds.has(id) && toolCalls[id]);

    if (liveIds.length === 0) {
      pushHistoryParts(parts, msg.content, historyTCs, toolCalls);
      return parts;
    }

    // Interleave: emit text segments between tool calls using snapshots.
    // textAtToolCallStart[callId] = assistant content when tool_call_start arrived.
    let prevTextEnd = 0;
    const fullText = msg.content ?? "";

    // Emit history tool calls (if any) after the initial text
    if (historyTCs.length > 0) {
      pushHistoryParts(parts, fullText, historyTCs, toolCalls);
      prevTextEnd = fullText.length;
    }

    // Interleave live tool calls with text segments
    for (const id of liveIds) {
      const tc = toolCalls[id];
      if (!tc) continue;
      const segmentEnd = (textAtToolCallStart[id] ?? "").length;
      if (segmentEnd > prevTextEnd) {
        parts.push({ type: "text", text: fullText.slice(prevTextEnd, segmentEnd) });
      }
      prevTextEnd = segmentEnd;
      parts.push({
        type: "tool-call",
        toolCallId: id,
        toolName: tc.tool,
        args: tc.params,
        result: tc.result,
      });
    }

    // Emit any remaining text after the last tool call
    if (prevTextEnd < fullText.length) {
      parts.push({ type: "text", text: fullText.slice(prevTextEnd) });
    } else if (parts.length === 0) {
      // Ensure at least one text part (needed for streaming placeholder)
      parts.push({ type: "text", text: fullText });
    }

    return parts;
  } catch (err) {
    console.error('[omnipus-runtime] buildContentParts failed:', err)
    return [{ type: "text", text: msg.content ?? "[Error rendering message]" }]
  }
}

function buildMessageStatus(msg: ChatMessage): ThreadMessageLike["status"] {
  if (msg.isStreaming) return { type: "running" };
  if (msg.status === "error") return { type: "incomplete", reason: "error" };
  if (msg.status === "interrupted") return { type: "incomplete", reason: "cancelled" };
  return { type: "complete", reason: "stop" };
}

function convertMessage(
  msg: ChatMessage,
  toolCalls: Record<string, StoreToolCall>,
  toolCallOrder: string[],
  textAtToolCallStart: Record<string, string>,
  isLastAssistant: boolean
): ThreadMessageLike {
  return {
    id: msg.id,
    role: msg.role,
    content: buildContentParts(msg, toolCalls, toolCallOrder, textAtToolCallStart, isLastAssistant),
    ...(msg.role === "assistant" ? { status: buildMessageStatus(msg) } : {}),
  };
}

// ── Runtime hook ──────────────────────────────────────────────────────────────

export function useOmnipusRuntime() {
  const messages = useChatStore((s) => s.messages);
  const toolCalls = useChatStore((s) => s.toolCalls);
  const toolCallOrder = useChatStore((s) => s.toolCallOrder);
  const textAtToolCallStart = useChatStore((s) => s.textAtToolCallStart);
  const isStreaming = useChatStore((s) => s.isStreaming);
  const sendMessage = useChatStore((s) => s.sendMessage);
  const cancelStream = useChatStore((s) => s.cancelStream);
  const addToast = useUiStore((s) => s.addToast);

  return useExternalStoreRuntime<ChatMessage>({
    messages,
    isRunning: isStreaming,
    convertMessage: (msg) => {
      // Check if this is the last assistant message — live tool calls from
      // the store are attached only to the last assistant message.
      const lastAssistantIdx = messages.map((m) => m.role).lastIndexOf('assistant');
      const isLastAssistant = lastAssistantIdx >= 0 && messages[lastAssistantIdx].id === msg.id;
      return convertMessage(msg, toolCalls, toolCallOrder, textAtToolCallStart, isLastAssistant);
    },
    onNew: async (message: AppendMessage) => {
      const textPart = message.content.find((p) => p.type === "text");
      if (!textPart) {
        console.warn("[omnipus-runtime] Message received without text content — skipping. Content types:", message.content.map((p) => p.type));
        addToast({ message: "Could not send — message contained no text content.", variant: "error" });
        return;
      }
      sendMessage(textPart.text);
    },
    onCancel: async () => { cancelStream() },
  });
}
