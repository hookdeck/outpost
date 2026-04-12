# Scenario 3 — Basics with Python

## Intent

Agent should produce a **single Python script** using `outpost_sdk`, equivalent to scenario 2.

## Preconditions

- Python 3.9+; `pip install outpost_sdk`.
- `OUTPOST_API_KEY`, `OUTPOST_TEST_WEBHOOK_URL` set.

## Automated eval (Claude Agent SDK)

The harness sets the agent **cwd** to `docs/agent-evaluation/results/runs/<stamp>-scenario-NN/`. Save `*.py`, `requirements.txt` or `pyproject.toml` with **Write** / **Edit**; use **Bash** for `pip` / `uv` installs so the run directory is self-contained.

## Conversation script

### Turn 0

Paste the **## Template** block from `[hookdeck-outpost-agent-prompt.mdoc](../../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc)`, with `{{…}}` filled using your project or `[fixtures/placeholder-values-for-turn0.md](../fixtures/placeholder-values-for-turn0.md)`.

### Turn 1 — User

> Option 1. I’d like to use **Python**.

### Turn 2 — User (optional)

> One file I can run with `python` is enough.

## Success criteria

**Measurement:** Heuristic `scoreScenario03` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge maps the checklist below ([README.md § Measuring scenarios](../README.md#measuring-scenarios)). Execution row is manual.

- `from outpost_sdk import Outpost` (or equivalent documented import path).
- `Outpost(api_key=..., server_url=...)` with optional base URL from env.
- `tenants.upsert`, `destinations.create`, `publish.event` as in the **Python quickstart** (including `request=` for publish where the SDK requires it).
- Topic aligned with prompt; webhook URL from env.
- No secrets in file.
- **Execution (full pass):** With `OUTPOST_API_KEY`, `OUTPOST_TEST_WEBHOOK_URL`, and optional base URL env vars set, `python …` (as documented) completes without API errors and prints an event id or clear success. *Skip only for transcript-only triage.*

## Failure modes to note

- Using `requests` only when user asked for the official SDK.