// AssistantUI runtime adapter — bridges the Zustand chat store + WebSocket protocol
// to useExternalStoreRuntime so AssistantUI primitives can render our messages.

import { useExternalStoreRuntime } from "@assistant-ui/react";
import type { ThreadMessageLike, AppendMessage } from "@assistant-ui/react";
import { useChatStore } from "@/store/chat";
import type { ChatMessage } from "@/store/chat";
import type { ToolCall } from "@/lib/api";

type StoreToolCall = ToolCall & { call_id: string };

// ── Message conversion ────────────────────────────────────────────────────────

function buildContentParts(
  msg: ChatMessage,
  toolCalls: Record<string, StoreToolCall>
): ThreadMessageLike["content"] {
  const parts: ThreadMessageLike["content"] = [];

  // Text content (always include, even if empty — needed for streaming placeholder)
  parts.push({ type: "text", text: msg.content });

  // Tool call parts — prefer live store data, fall back to embedded message data
  for (const tc of msg.tool_calls ?? []) {
    const resolved: ToolCall = toolCalls[tc.id] ?? tc;
    parts.push({
      type: "tool-call",
      toolCallId: tc.id,
      toolName: tc.tool,
      args: tc.params,
      result: resolved.result,
    });
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
  toolCalls: Record<string, StoreToolCall>
): ThreadMessageLike {
  const base: ThreadMessageLike = {
    id: msg.id,
    role: msg.role,
    content: buildContentParts(msg, toolCalls),
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

  return useExternalStoreRuntime<ChatMessage>({
    messages,
    isRunning: isStreaming,
    convertMessage: (msg) => convertMessage(msg, toolCalls),
    onNew: (message: AppendMessage) => {
      const textPart = message.content.find((p) => p.type === "text");
      if (!textPart || textPart.type !== "text") return;
      sendMessage(textPart.text);
    },
    onCancel: cancelStream,
  });
}
