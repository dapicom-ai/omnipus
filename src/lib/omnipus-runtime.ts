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

function buildContentParts(
  msg: ChatMessage,
  toolCalls: Record<string, StoreToolCall>,
  isLastAssistant: boolean
): ThreadMessageLike["content"] {
  const parts: ThreadMessageLike["content"] = [];

  // Text content (always include, even if empty — needed for streaming placeholder)
  parts.push({ type: "text", text: msg.content });

  // Tool call parts from embedded message data (history)
  const seenIds = new Set<string>();
  for (const tc of msg.tool_calls ?? []) {
    seenIds.add(tc.id);
    const resolved: ToolCall = toolCalls[tc.id] ?? tc;
    parts.push({
      type: "tool-call",
      toolCallId: tc.id,
      toolName: tc.tool,
      args: tc.params,
      result: resolved.result,
    });
  }

  // For the last assistant message, also include live tool calls from the store
  // that aren't already in the message's tool_calls array.
  // This is needed because tool_call_start/result WebSocket frames update the
  // store but don't embed into the message object during streaming.
  if (isLastAssistant && msg.role === "assistant") {
    for (const [id, tc] of Object.entries(toolCalls)) {
      if (seenIds.has(id)) continue;
      parts.push({
        type: "tool-call",
        toolCallId: id,
        toolName: tc.tool,
        args: tc.params,
        result: tc.result,
      });
    }
  }

  return parts;
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
  isLastAssistant: boolean
): ThreadMessageLike {
  const base: ThreadMessageLike = {
    id: msg.id,
    role: msg.role,
    content: buildContentParts(msg, toolCalls, isLastAssistant),
  };
  // status is only supported on assistant messages
  if (msg.role === "assistant") {
    base.status = buildMessageStatus(msg);
  }
  return base;
}

// ── Runtime hook ──────────────────────────────────────────────────────────────

export function useOmnipusRuntime() {
  const messages = useChatStore((s) => s.messages);
  const toolCalls = useChatStore((s) => s.toolCalls);
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
      return convertMessage(msg, toolCalls, isLastAssistant);
    },
    onNew: (message: AppendMessage) => {
      const textPart = message.content.find((p) => p.type === "text");
      if (!textPart || textPart.type !== "text") {
        console.warn("[omnipus-runtime] Message received without text content — skipping. Content types:", message.content.map((p) => p.type));
        addToast({ message: "Could not send — message contained no text content.", variant: "error" });
        return;
      }
      sendMessage(textPart.text);
    },
    onCancel: cancelStream,
  });
}
