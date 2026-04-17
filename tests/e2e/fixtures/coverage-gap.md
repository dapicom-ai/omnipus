# Coverage Gaps — deferred to follow-up PR

The following §1 acceptance criteria are not covered in this fix pass.
Each is deferred because the scope is too large or requires app-side signal additions.

- Session title collision blocked within same agent
- 5 malformed WS frames → disconnect + auto-reconnect
- Tool approval "Always allow" persists per-(agent, tool) — current test only checks global persistence
- Cron inbound message toast + session-panel badge
- Provider failover UX indicator
- Subagent grandchildren (unlimited depth)
- Approval auto-deny at 5 min
- Concurrent /onboarding/complete 409 race
