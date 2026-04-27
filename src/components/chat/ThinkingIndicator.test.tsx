// ThinkingIndicator is not a standalone component — it renders inline inside MessageItem
// when message.isStreaming is true and message.content is empty.
// All test #4 scenarios are covered in MessageItem.test.tsx.
// Traces to: wave5a-wire-ui-spec.md — Scenario: Thinking indicator shows before first token

import { describe, it, expect } from 'vitest'

describe('ThinkingIndicator — covered by MessageItem.test.tsx (test #4)', () => {
  it('thinking indicator tests live in MessageItem.test.tsx', () => {
    // See: src/components/chat/MessageItem.test.tsx — describe('MessageItem — thinking indicator')
    expect(true).toBe(true)
  })
})
