# Repository Secrets

This document lists every GitHub Actions secret the CI/CD workflows require.
Add each secret under **Settings > Secrets and variables > Actions > Repository secrets**.

---

## Playwright E2E (`pr.yml` — `playwright` job)

### `OPENROUTER_API_KEY_CI`

Used by the Playwright E2E job to start the Omnipus gateway with a live LLM
backend so that chat and agent tests receive real (not mocked) responses.

**Recommended model:** `z-ai/glm-5-turbo` or `google/gemini-2.5-flash` via
OpenRouter — both support tool use and cost well under $0.01 per Playwright run.

**Suggested monthly cap:** $5. Set a usage limit in your OpenRouter dashboard
under the key's settings to prevent runaway spend if a CI job loops.

**Minimum permissions:** The key only needs `chat.completions` scope. Do not
reuse a key that has billing-write or organisation-admin scope.

---

## Nightly Evals (`evals-nightly.yml`)

### `OPENROUTER_API_KEY_EVAL`

Used by the nightly eval runner for two calls per scenario: one to the agent
model (`z-ai/glm-5-turbo` or `google/gemini-2.5-flash`) and one to the judge
model (`anthropic/claude-sonnet-4.6`). 15 scenarios per run ≈ $0.30–$0.80/night.

This can be the same key as `OPENROUTER_API_KEY_CI` or a separate key with a
higher monthly cap (suggested: $25) to accommodate the stronger judge model.

### `OMNIPUS_MASTER_KEY_EVAL`

64-character hex-encoded 256-bit AES master key used to encrypt the ephemeral
credential store spun up per-scenario. Each nightly run creates fresh
per-scenario home directories; this key is discarded after the run. Any valid
64-char hex string works — it does not need to match any production key.

Generate with:

```bash
openssl rand -hex 32
```

Copy the output (exactly 64 hex chars, 0-9 / a-f) into the secret value.

### Budget

The nightly eval run is designed to cost $0.30–$0.80 per run against the
default 15 scenarios. The runner exits non-zero and fails the workflow if
cost exceeds $2.00 in a single run.

---

## Rotation policy

Rotate all OpenRouter keys at least every 90 days. After rotation, update
the secret value in GitHub Actions and verify the next CI run passes before
closing the rotation ticket. The `OMNIPUS_MASTER_KEY_EVAL` value is not
sensitive across rotations — regenerate freely.
