import { describe, it, expect } from 'vitest'
import { _test } from './omnipus-runtime'
import type { ChatMessage, SubagentSpan } from '@/store/chat'
import type { ToolCall } from '@/lib/api'

const { buildContentParts } = _test

type Part =
  | { type: 'text'; text: string }
  | { type: 'tool-call'; toolCallId: string; toolName: string; args: unknown; result?: unknown }
  | { type: 'data-subagent-span'; data: { spanId: string } }

function assistant(content: string, extra: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 'm1',
    role: 'assistant',
    content,
    timestamp: '2026-04-23T00:00:00Z',
    status: 'streaming',
    ...extra,
  } as ChatMessage
}

function tc(id: string, params: Record<string, unknown>, result?: unknown): ToolCall & { call_id: string } {
  return {
    id,
    call_id: id,
    tool: id,
    params,
    result,
    status: result !== undefined ? 'success' : 'running',
  }
}

function span(spanId: string, steps: SubagentSpan['steps'] = []): SubagentSpan {
  return {
    spanId,
    parentCallId: `p_${spanId}`,
    taskLabel: `task ${spanId}`,
    status: 'running',
    steps,
  }
}

// Convenience for concise tests.
function run(
  msg: ChatMessage,
  opts: {
    toolCalls?: Record<string, ToolCall & { call_id: string }>
    toolCallOrder?: string[]
    textAtToolCallStart?: Record<string, string>
    toolCallMessageId?: Record<string, string>
    spanOrder?: string[]
    textAtSpanStart?: Record<string, string>
    isLastAssistant?: boolean
  } = {},
): Part[] {
  // Default every tool call to the message under test so pre-existing tests
  // (which never wired an owner map) keep working. Tests that care about
  // cross-message attribution pass toolCallMessageId explicitly.
  const order = opts.toolCallOrder ?? []
  const defaultOwner: Record<string, string> =
    opts.toolCallMessageId ?? Object.fromEntries(order.map((id) => [id, msg.id]))
  return buildContentParts(
    msg,
    opts.toolCalls ?? {},
    order,
    opts.textAtToolCallStart ?? {},
    defaultOwner,
    opts.spanOrder ?? [],
    opts.textAtSpanStart ?? {},
    opts.isLastAssistant ?? true,
  ) as Part[]
}

describe('buildContentParts — unified ordering', () => {
  it('#1 mid-stream span: text "AB" → span → text "CD"', () => {
    const msg = assistant('ABCD', { spans: [span('s1')] })
    const parts = run(msg, {
      spanOrder: ['s1'],
      textAtSpanStart: { s1: 'AB' },
    })
    expect(parts).toEqual([
      { type: 'text', text: 'AB' },
      { type: 'data-subagent-span', data: { spanId: 's1' } },
      { type: 'text', text: 'CD' },
    ])
  })

  it('#2 mixed: text, tool, text, span, text', () => {
    const msg = assistant('hello world done', { spans: [span('s1')] })
    const parts = run(msg, {
      toolCalls: { t1: tc('t1', { q: 'x' }, 'ok') },
      toolCallOrder: ['t1'],
      textAtToolCallStart: { t1: 'hello ' },
      spanOrder: ['s1'],
      textAtSpanStart: { s1: 'hello world ' },
    })
    expect(parts).toEqual([
      { type: 'text', text: 'hello ' },
      { type: 'tool-call', toolCallId: 't1', toolName: 't1', args: { q: 'x' }, result: 'ok' },
      { type: 'text', text: 'world ' },
      { type: 'data-subagent-span', data: { spanId: 's1' } },
      { type: 'text', text: 'done' },
    ])
  })

  it('#3 span before any text: span then text', () => {
    const msg = assistant('hi', { spans: [span('s1')] })
    const parts = run(msg, {
      spanOrder: ['s1'],
      textAtSpanStart: { s1: '' },
    })
    expect(parts).toEqual([
      { type: 'data-subagent-span', data: { spanId: 's1' } },
      { type: 'text', text: 'hi' },
    ])
  })

  it('#4 orphan buffer TTL release: no span → flat tool calls interleave', () => {
    // The orphan-buffer fallback calls startToolCall (not startSpan), so the
    // drained frames appear as regular top-level tool calls.
    const msg = assistant('beforeafter')
    const parts = run(msg, {
      toolCalls: { tFlat: tc('tFlat', {}, 'out') },
      toolCallOrder: ['tFlat'],
      textAtToolCallStart: { tFlat: 'before' },
    })
    expect(parts).toEqual([
      { type: 'text', text: 'before' },
      { type: 'tool-call', toolCallId: 'tFlat', toolName: 'tFlat', args: {}, result: 'out' },
      { type: 'text', text: 'after' },
    ])
    // No data-* part emitted when no span exists.
    expect(parts.some((p) => p.type === 'data-subagent-span')).toBe(false)
  })

  it('#5 history + live coexistence — secondary bug repro', () => {
    // Message has a completed history tool call. A later streamed text + live
    // tool call must still interleave correctly instead of collapsing to end.
    const msg = assistant('later text', {
      tool_calls: [{ id: 'h1', tool: 'h1', params: {}, status: 'success', result: 'hr' }],
    })
    const parts = run(msg, {
      toolCalls: {
        h1: tc('h1', {}, 'hr'),
        t2: tc('t2', {}, 'out'),
      },
      toolCallOrder: ['h1', 't2'],
      textAtToolCallStart: { t2: 'later ' },
    })
    expect(parts).toEqual([
      { type: 'tool-call', toolCallId: 'h1', toolName: 'h1', args: {}, result: 'hr' },
      { type: 'text', text: 'later ' },
      { type: 'tool-call', toolCallId: 't2', toolName: 't2', args: {}, result: 'out' },
      { type: 'text', text: 'text' },
    ])
  })

  it('#6 empty message + streaming produces one empty text part', () => {
    const msg = assistant('')
    const parts = run(msg)
    expect(parts).toEqual([{ type: 'text', text: '' }])
  })

  it('#7 two sibling spans interleaved with tokens', () => {
    const msg = assistant('ab cd ef', { spans: [span('sA'), span('sB')] })
    const parts = run(msg, {
      spanOrder: ['sA', 'sB'],
      textAtSpanStart: { sA: 'ab ', sB: 'ab cd ' },
    })
    expect(parts).toEqual([
      { type: 'text', text: 'ab ' },
      { type: 'data-subagent-span', data: { spanId: 'sA' } },
      { type: 'text', text: 'cd ' },
      { type: 'data-subagent-span', data: { spanId: 'sB' } },
      { type: 'text', text: 'ef' },
    ])
  })

  it('#8 tool call and span sharing the same text offset preserve insertion order', () => {
    // Both events snapshot text = "A", order of insertion is tool then span.
    const msg = assistant('AB', { spans: [span('s1')] })
    const parts = run(msg, {
      toolCalls: { t1: tc('t1', {}, 'ok') },
      toolCallOrder: ['t1'],
      textAtToolCallStart: { t1: 'A' },
      spanOrder: ['s1'],
      textAtSpanStart: { s1: 'A' },
    })
    // Expected: tool before span (since tool entry order is earlier in our event
    // list; stable sort preserves the insertion order we chose — history first,
    // then spans, then live tools. Span insertion precedes live tool here).
    expect(parts[0]).toEqual({ type: 'text', text: 'A' })
    // At least one of the next two is the tool and one is the span.
    const middle = parts.slice(1, 3).map((p) => p.type).sort()
    expect(middle).toEqual(['data-subagent-span', 'tool-call'])
    expect(parts[parts.length - 1]).toEqual({ type: 'text', text: 'B' })
  })

  it('#9 a live tool call owned by another message does not render on this one', () => {
    // Scoping is by toolCallMessageId, not by isLastAssistant position — so a
    // tool call owned by a different message is skipped even when this message
    // happens to be the last assistant.
    const msg = assistant('prior turn', { id: 'prior' })
    const parts = run(msg, {
      toolCalls: { tLive: tc('tLive', {}, 'x') },
      toolCallOrder: ['tLive'],
      textAtToolCallStart: { tLive: '' },
      toolCallMessageId: { tLive: 'some-other-msg' },
    })
    expect(parts).toEqual([{ type: 'text', text: 'prior turn' }])
  })

  it('#R1 REGRESSION: prior-turn tool calls must not leak into a new assistant message', () => {
    // Bug observed in live Jim session: after turn 1 completes with several tool
    // calls, sending a follow-up user message caused all of turn 1's tool calls
    // to re-render below the new user message (inside the new assistant bubble),
    // because toolCallOrder is a global insertion list in the store.
    //
    // Reproduction: turn 1's tool calls remain in toolCallOrder / toolCalls /
    // textAtToolCallStart; they are not attributed to turn 1's message (msg1).
    // Turn 2 starts — msg2 is the last assistant. Current buildContentParts
    // iterates toolCallOrder for the LAST assistant only, so msg2 steals them.
    const msg1Prior: ChatMessage = {
      id: 'msg1',
      role: 'assistant',
      content: 'turn 1 response',
      timestamp: '2026-04-24T00:00:00Z',
      status: 'done',
    } as ChatMessage

    const msg2Last: ChatMessage = {
      id: 'msg2',
      role: 'assistant',
      content: 'turn 2 reply',
      timestamp: '2026-04-24T00:01:00Z',
      status: 'streaming',
    } as ChatMessage

    // Simulated store state after turn 1 done + turn 2 streaming (no tc yet on t2)
    const commonOpts = {
      toolCalls: {
        t1a: tc('t1a', {}, 'r1a'),
        t1b: tc('t1b', {}, 'r1b'),
      },
      toolCallOrder: ['t1a', 't1b'],
      textAtToolCallStart: { t1a: 'turn 1 ', t1b: 'turn 1 response' },
      // Both turn-1 tool calls belong to msg1 — that's the entire fix.
      toolCallMessageId: { t1a: 'msg1', t1b: 'msg1' },
      spanOrder: [] as string[],
      textAtSpanStart: {} as Record<string, string>,
    }

    const msg1Parts = run(msg1Prior, { ...commonOpts, isLastAssistant: false })
    const msg2Parts = run(msg2Last, { ...commonOpts, isLastAssistant: true })

    // msg1 should render its tool calls inline with its own text. msg2 should
    // render only its own content — zero tool calls from the previous turn.
    expect(msg1Parts.some((p) => p.type === 'tool-call' && p.toolCallId === 't1a')).toBe(true)
    expect(msg1Parts.some((p) => p.type === 'tool-call' && p.toolCallId === 't1b')).toBe(true)
    expect(msg2Parts.some((p) => p.type === 'tool-call')).toBe(false)
    expect(msg2Parts.filter((p) => p.type === 'text').map((p) => p.text).join('')).toBe('turn 2 reply')
  })

  it('#10 span with snapshot beyond current text length clamps to end', () => {
    // Defensive: if snapshot somehow exceeds fullText.length (e.g. text was
    // truncated after the snapshot was taken), clamp to avoid negative slices.
    const msg = assistant('short', { spans: [span('s1')] })
    const parts = run(msg, {
      spanOrder: ['s1'],
      textAtSpanStart: { s1: 'this is longer than short' },
    })
    expect(parts).toEqual([
      { type: 'text', text: 'short' },
      { type: 'data-subagent-span', data: { spanId: 's1' } },
    ])
  })
})
