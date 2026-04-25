// AssistantUI runtime adapter — bridges the Zustand chat store + WebSocket protocol
// to useExternalStoreRuntime so AssistantUI primitives can render our messages.

import { useExternalStoreRuntime } from "@assistant-ui/react";
import type { ThreadMessageLike, AppendMessage } from "@assistant-ui/react";
import { useChatStore } from "@/store/chat";
import type { ChatMessage } from "@/store/chat";
import type { ToolCall } from "@/lib/api";
import { useUiStore } from "@/store/ui";

type StoreToolCall = ToolCall & { call_id: string };
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type ContentPart = any;

// ── Message conversion ────────────────────────────────────────────────────────

// An event to interleave between text slices. All events carry a `textOffset`
// into the message's full text content; a stable sort groups them in the order
// they actually appeared in the stream.
type InterleaveEvent =
  | { textOffset: number; order: number; kind: 'tool'; id: string; source: 'history' | 'live' }
  | { textOffset: number; order: number; kind: 'span'; id: string };

function resolveToolPart(id: string, tc: ToolCall | undefined, toolCalls: Record<string, StoreToolCall>): ContentPart {
  // Prefer the live store entry so results materialise as they arrive.
  const resolved = toolCalls[id] ?? tc;
  return {
    type: "tool-call",
    toolCallId: id,
    toolName: resolved?.tool ?? '',
    args: resolved?.params ?? {},
    result: resolved?.result,
  };
}

function spanPart(id: string): ContentPart {
  // AssistantUI strips the "data-" prefix and routes to components.data.by_name['subagent-span'].
  // Only the spanId is embedded — the SubagentSpanPart component subscribes to the
  // store to pick up step updates without triggering a full content re-conversion.
  return { type: 'data-subagent-span', data: { spanId: id } };
}

function buildContentParts(
  msg: ChatMessage,
  toolCalls: Record<string, StoreToolCall>,
  toolCallOrder: string[],
  textAtToolCallStart: Record<string, string>,
  toolCallMessageId: Record<string, string>,
  spanOrder: string[],
  textAtSpanStart: Record<string, string>,
  _isLastAssistant: boolean
): ThreadMessageLike["content"] {
  try {
    const fullText = msg.content ?? "";

    // Non-assistant messages have no tool calls or spans — just text.
    if (msg.role !== 'assistant') {
      return [{ type: 'text', text: fullText }];
    }

    const historyTCs = msg.tool_calls ?? [];
    const historyTCIds = new Set(historyTCs.map((tc) => tc.id));
    const msgSpans = msg.spans ?? [];
    const msgSpanIds = new Set(msgSpans.map((s) => s.spanId));

    // Build the unified event list. `order` preserves insertion order so a
    // stable sort by (textOffset, order) reproduces the stream sequence when
    // events share a text position.
    const events: InterleaveEvent[] = [];
    let order = 0;

    // 1. History tool calls — offset 0. These were emitted before any snapshot
    //    machinery existed for this message; grouping them at offset 0 preserves
    //    server-order and matches prior behaviour for replayed/prior-turn messages.
    for (const tc of historyTCs) {
      events.push({ textOffset: 0, order: order++, kind: 'tool', id: tc.id, source: 'history' });
    }

    // 2. Spans attached to this message — offset = textAtSpanStart snapshot length.
    //    Filter against this message's spanIds so prior-message spans don't leak in
    //    when spanOrder / textAtSpanStart are store-global.
    const spanIdsInOrder = spanOrder.filter((id) => msgSpanIds.has(id));
    for (const id of spanIdsInOrder) {
      const offset = (textAtSpanStart[id] ?? '').length;
      events.push({ textOffset: offset, order: order++, kind: 'span', id });
    }
    // Fallback: if any span on this message is missing from spanOrder (e.g., on
    // a future refactor), still emit it so it doesn't silently disappear.
    for (const span of msgSpans) {
      if (!spanOrder.includes(span.spanId)) {
        const offset = (textAtSpanStart[span.spanId] ?? '').length;
        events.push({ textOffset: offset, order: order++, kind: 'span', id: span.spanId });
      }
    }

    // 3. Live tool calls — attributed to the assistant message that was last
    //    when each tool_call_start arrived. toolCallOrder is global, so without
    //    messageId scoping a new turn's message would steal every prior turn's
    //    tool call (repro: see buildContentParts test #R1).
    for (const id of toolCallOrder) {
      if (toolCallMessageId[id] !== msg.id) continue;
      if (historyTCIds.has(id) || !toolCalls[id]) continue;
      const offset = (textAtToolCallStart[id] ?? '').length;
      events.push({ textOffset: offset, order: order++, kind: 'tool', id, source: 'live' });
    }

    // Stable sort by (textOffset, order) — Array.prototype.sort is stable in
    // modern JS engines, but we compare `order` as a tiebreaker to be explicit.
    events.sort((a, b) => a.textOffset - b.textOffset || a.order - b.order);

    const parts: ContentPart[] = [];
    let cursor = 0;
    for (const ev of events) {
      const clampedOffset = Math.min(Math.max(ev.textOffset, cursor), fullText.length);
      if (clampedOffset > cursor) {
        parts.push({ type: 'text', text: fullText.slice(cursor, clampedOffset) });
        cursor = clampedOffset;
      }
      if (ev.kind === 'tool') {
        const historyTc = ev.source === 'history' ? historyTCs.find((t) => t.id === ev.id) : undefined;
        parts.push(resolveToolPart(ev.id, historyTc, toolCalls));
      } else {
        parts.push(spanPart(ev.id));
      }
    }

    // Emit any trailing text after the last event.
    if (cursor < fullText.length) {
      parts.push({ type: 'text', text: fullText.slice(cursor) });
    }

    // AssistantUI needs at least one part per message — a streaming placeholder
    // with no text / tool calls / spans yet must still render the "thinking" UI.
    if (parts.length === 0) {
      parts.push({ type: 'text', text: fullText });
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
  toolCallMessageId: Record<string, string>,
  spanOrder: string[],
  textAtSpanStart: Record<string, string>,
  isLastAssistant: boolean
): ThreadMessageLike {
  return {
    id: msg.id,
    role: msg.role,
    content: buildContentParts(msg, toolCalls, toolCallOrder, textAtToolCallStart, toolCallMessageId, spanOrder, textAtSpanStart, isLastAssistant),
    ...(msg.role === "assistant" ? { status: buildMessageStatus(msg) } : {}),
  };
}

// Exported for unit testing — covers the core ordering algorithm without
// needing to mount the AssistantUI runtime.
export const _test = { buildContentParts };

// ── Runtime hook ──────────────────────────────────────────────────────────────

export function useOmnipusRuntime() {
  const messages = useChatStore((s) => s.messages);
  const toolCalls = useChatStore((s) => s.toolCalls);
  const toolCallOrder = useChatStore((s) => s.toolCallOrder);
  const textAtToolCallStart = useChatStore((s) => s.textAtToolCallStart);
  const toolCallMessageId = useChatStore((s) => s.toolCallMessageId);
  const spanOrder = useChatStore((s) => s.spanOrder);
  const textAtSpanStart = useChatStore((s) => s.textAtSpanStart);
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
      return convertMessage(msg, toolCalls, toolCallOrder, textAtToolCallStart, toolCallMessageId, spanOrder, textAtSpanStart, isLastAssistant);
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
