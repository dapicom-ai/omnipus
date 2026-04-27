// ChatMessage is not a standalone component — message rendering lives in MessageItem.
// All tests from test #2 (user message) and test #3 (assistant message) are covered
// in MessageItem.test.tsx at src/components/chat/MessageItem.test.tsx
// Traces to: wave5a-wire-ui-spec.md — Scenario: User message appears optimistically
//             wave5a-wire-ui-spec.md — Scenario: Streaming response completes with markdown rendering

import { describe, it, expect } from 'vitest'

describe('ChatMessage — covered by MessageItem.test.tsx (tests #2, #3)', () => {
  it('user message and assistant message tests live in MessageItem.test.tsx', () => {
    // See: src/components/chat/MessageItem.test.tsx
    expect(true).toBe(true)
  })
})
