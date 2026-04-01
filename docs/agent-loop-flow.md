# Agent Loop Event Flow — Mermaid Diagram

```mermaid
sequenceDiagram
    participant Browser
    participant WS as WSHandler
    participant Bus as MessageBus
    participant AL as AgentLoop.Run()
    participant PM as processMessage()
    participant RAL as runAgentLoop()
    participant RT as runTurn()
    participant LLM as callLLM()
    participant Provider as OpenAI/OpenRouter
    participant Streamer as wsStreamer
    participant WCC as webchatChannel
    participant Tools as ToolRegistry

    Note over Browser,WS: === Phase 1: Message Delivery ===
    Browser->>WS: WebSocket frame {type:"message", content:"...", agent_id:"..."}
    WS->>WS: handleChatMessage() — create session, record user msg
    WS->>Bus: PublishInbound(InboundMessage{channel:"webchat", chatID:"webchat:uuid"})

    Note over Bus,AL: === Phase 2: Message Processing ===
    Bus-->>AL: InboundChan() delivers msg
    AL->>AL: resolveSteeringTarget() + drainBusToSteering()
    AL->>PM: processMessage(ctx, msg)
    PM->>PM: resolveMessageRoute() → agent, route
    PM->>PM: ResetSentInRound() on message tool
    PM->>RAL: runAgentLoop(ctx, agent, opts{SendResponse:false})
    RAL->>RT: runTurn(ctx, turnState)

    Note over RT,Provider: === Phase 3: Turn Loop (iteration N) ===
    RT->>RT: BuildSystemPrompt() + load session history
    RT->>RT: providerToolDefs = agent.Tools.ToProviderDefs()
    RT->>LLM: callLLM(messages, toolDefs)

    Note over LLM,Streamer: === Phase 3a: Streaming Path ===
    LLM->>LLM: Check: provider implements StreamingProvider?
    LLM->>Bus: bus.GetStreamer(ctx, "webchat", chatID)
    Bus->>WS: WSHandler.GetStreamer() → new wsStreamer{conn, chatID}
    LLM->>Provider: ChatStream(ctx, messages, tools, model, opts, onChunk)

    loop SSE token chunks
        Provider-->>LLM: SSE data chunk (delta text)
        LLM->>Streamer: streamer.Update(delta)
        Streamer->>Browser: WS frame {type:"token", content:delta}
    end

    Provider-->>LLM: Final SSE chunk (finish_reason + tool_calls)
    LLM->>LLM: Parse complete response
    LLM->>Streamer: streamer.Finalize() → sends {type:"done"}
    Note over Streamer,WCC: Finalize also calls webchatCh.markStreamed(chatID)
    Streamer->>Browser: WS frame {type:"done"}
    LLM-->>RT: return (response, nil)

    Note over RT,Tools: === Phase 3b: Tool Call Decision ===
    alt Response has NO tool calls
        RT->>RT: finalContent = response.Content
        RT->>RT: break (exit turn loop)
    else Response HAS tool calls
        RT->>RT: Normalize tool calls, add assistant msg to history

        loop For each tool call
            RT->>RT: hooks.BeforeTool() — may modify/deny
            RT->>RT: hooks.ApproveTool() — interactive approval
            Note over RT,Browser: ApproveTool sends exec_approval_request WS frame<br/>Browser responds with allow/deny/always
            alt Tool auto-approved (autoApproveSafeTool)
                RT->>RT: Skip approval dialog
            else Tool needs approval
                RT->>Browser: WS frame {type:"exec_approval_request"}
                Browser-->>RT: WS frame {type:"exec_approval_response", decision:"allow"}
            end

            RT->>RT: emitEvent(ToolExecStart) → eventForwarder
            Note over RT,Browser: eventForwarder sends {type:"tool_call_start"} WS frame
            RT->>Tools: ExecuteWithContext(toolName, args)
            Tools-->>RT: ToolResult
            RT->>RT: emitEvent(ToolExecEnd) → eventForwarder
            Note over RT,Browser: eventForwarder sends {type:"tool_call_result"} WS frame
            RT->>RT: Add tool result message to history
        end

        RT->>RT: Continue turn loop → goto Phase 3 (next iteration)
    end

    Note over RT,RAL: === Phase 4: Turn Completion ===
    RT-->>RAL: return turnResult{finalContent:"...", status:completed}

    alt opts.SendResponse == true
        RAL->>Bus: PublishOutbound(finalContent)
        Note right of RAL: BUT SendResponse is FALSE for webchat!
    end

    RAL-->>PM: return finalContent
    PM-->>AL: return response string

    Note over AL,Browser: === Phase 5: Response Publication ===
    AL->>AL: finalResponse = response
    AL->>AL: buildContinuationTarget(msg) → target{channel,chatID}

    alt target == nil (system channel)
        AL->>AL: publishResponseIfNeeded(msg.Channel, msg.ChatID, finalResponse)
    else target != nil (webchat)
        AL->>AL: Check pending steering → drain loop
        AL->>AL: publishResponseIfNeeded(target.Channel, target.ChatID, finalResponse)
    end

    Note over AL,WCC: === Phase 5a: publishResponseIfNeeded ===
    AL->>AL: Check HasSentInRound() on default agent's message tool
    alt Already sent by message tool
        AL->>AL: Skip (no double publish)
    else Not sent yet
        AL->>Bus: PublishOutbound(OutboundMessage{channel:"webchat", chatID, content})
        Bus->>Bus: outbound channel
        Bus-->>WCC: Channel Manager dispatches to webchatChannel

        alt webchatChannel.streamed[chatID] == true
            WCC->>WCC: Skip Send() — already streamed via wsStreamer
            Note right of WCC: This is the NORMAL path when streaming worked
        else streamed flag NOT set
            WCC->>WS: sendConnFrame(token + done frames)
            WS->>Browser: WS frames {type:"token"} + {type:"done"}
            Note right of WCC: This is the FALLBACK for non-streaming
        end
    end

    Note over AL,Browser: === Phase 5b: Deferred Response Guard (FR-004) ===
    Note over AL: defer func() checks: if finalResponse != "" && !published<br/>→ calls publishResponseIfNeeded as safety net
```

## Critical Path Analysis

### The "done" frame problem:
1. `Finalize()` is called after EVERY `ChatStream()` — even when tool calls follow
2. Browser receives `{type:"done"}` → sets `isStreaming=false`, message status = "complete"
3. Tools execute (invisible to user if auto-approved)
4. Next `ChatStream()` sends new tokens → browser creates NEW assistant message? Or appends to old?
5. Second `Finalize()` → second `{type:"done"}`

### Key Question: What happens when tokens arrive AFTER a "done" frame?
- `updateLastAssistantMessage(content, false)` — finds last assistant message, appends content
- But `isStreaming` was already set to `false` by the first "done"
- The message appears "complete" then suddenly gets more text — jarring UX but functional

### The REAL gap: publishResponseIfNeeded after streaming
- After turn completes, `publishResponseIfNeeded` calls `PublishOutbound`
- `webchatChannel.Send()` checks `streamed[chatID]` — was it set?
- `Finalize()` calls `webchatCh.markStreamed(chatID)` — YES, it was set on last Finalize
- So `Send()` is a no-op — correct, response was already streamed
- BUT: `markStreamed` also has `delete(c.streamed, msg.ChatID)` in Send() — consumes the flag
- If Send() is called TWICE (deferred + explicit), second call goes through!

### Missing: What happens between iterations?
- Iteration 1: LLM streams text + tool calls → Finalize → "done" → tools execute
- Iteration 2: LLM streams response → Finalize → "done" → markStreamed
- publishResponseIfNeeded → Send() → streamed=true → skip (correct)
- Deferred guard → already published=true → skip (correct)

### Conclusion: The flow should work. The "model does nothing" issue is NOT in the agent loop logic.
