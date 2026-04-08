# Scenario 2 — Basics with TypeScript

## Intent

Agent should produce a **single runnable `.ts` file** using `@hookdeck/outpost-sdk`, following the managed TypeScript quickstart pattern.

## Preconditions

- Node 18+; user can run `npx tsx`.
- `OUTPOST_API_KEY` and `OUTPOST_TEST_WEBHOOK_URL` available as env vars.

## Automated eval (Claude Agent SDK)

The harness sets the agent **cwd** to `docs/agent-evaluation/results/runs/<stamp>-scenario-NN/`. Write the script and any `package.json` there with **Write** / **Edit**; use **Bash** for `npm install`, `npx tsx`, etc., so the folder is a runnable mini-project.

## Conversation script

### Turn 0

Paste the **## Template** block from `[hookdeck-outpost-agent-prompt.mdx](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx)`, with `{{…}}` filled using your project or `[fixtures/placeholder-values-for-turn0.md](../fixtures/placeholder-values-for-turn0.md)`.

### Turn 1 — User

> Option 1. Let’s do it in **TypeScript**.

### Turn 2 — User (optional)

> How do I run it locally?

## Success criteria

**Measurement:** Heuristic `scoreScenario02` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge maps the bullets below ([README.md § Measuring scenarios](../README.md#measuring-scenarios)). Execution row is manual.

- Depends on `@hookdeck/outpost-sdk`; uses `Outpost` client with `apiKey` from `process.env.OUTPOST_API_KEY`.
- Calls `tenants.upsert`, `destinations.create` (webhook), `publish.event`.
- Uses a topic that matches the dashboard list from the prompt (or asks which topic if ambiguous).
- Webhook URL from `OUTPOST_TEST_WEBHOOK_URL` (or clearly documented env).
- No API key in source; fails fast if env missing.
- Mentions `npx tsx script.ts` or equivalent run instructions.
- **Execution (full pass):** With `OUTPOST_API_KEY`, `OUTPOST_TEST_WEBHOOK_URL`, and optional `OUTPOST_API_BASE_URL` set, the generated script runs to completion (no uncaught API errors) and prints or logs an event id or other clear success signal. *Skip only for transcript-only triage.*

## Failure modes to note

- Defaulting to localhost API without user asking for self-hosted.
- Using raw `fetch` when user asked for TypeScript SDK specifically.