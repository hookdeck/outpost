# Scenario 5 — Minimal example app (Next.js)

## Intent

Agent scaffolds a **minimal Next.js** app (App Router or Pages Router acceptable) with a **simple UI** that lets an operator:

1. Register a **webhook destination** for a tenant (URL input + submit).
2. After registration, **trigger a test publish** to a configured topic so the destination receives an event.

Server-side code must call Outpost with the API key from **environment** (e.g. `OUTPOST_API_KEY`), never exposed to the browser.

## Preconditions

- User has Node 18+; comfortable creating a Next app.
- `OUTPOST_API_KEY`, managed base URL, at least one topic, and `OUTPOST_TEST_WEBHOOK_URL` or user-supplied URL pattern documented.

## Automated eval (Claude Agent SDK)

The harness sets the agent **cwd** to an empty directory under `docs/agent-evaluation/results/runs/<stamp>-scenario-NN/`. You **must** scaffold the Next.js app **into that directory** (e.g. `npx create-next-app@latest` with flags for non-interactive use) using **Bash**, then implement routes/server code with **Write** / **Edit**. Chat-only snippets are not enough for this scenario—the run folder should contain a real project tree reviewers can `npm install && npm run dev`.

## Conversation script

### Turn 0

Paste the **## Template** block from `[hookdeck-outpost-agent-prompt.mdoc](../../agent-evaluation/hookdeck-outpost-agent-prompt.md)`, with `{{…}}` filled using your project or `[fixtures/placeholder-values-for-turn0.md](../fixtures/placeholder-values-for-turn0.md)`.

### Turn 1 — User

> Option 2 — a **tiny demo app**. Can we use **Next.js**? I want a minimal page: somewhere to put a webhook URL, register it for a customer, and a way to fire one test event.

### Turn 2 — User (optional)

> Can you add a short README — what goes in `.env` and how I start the dev server?

### Turn 3 — User (stress)

> I don’t have a public webhook URL yet. What should I put in that field?

_Expected:_ agent points to a Hookdeck Console Source URL (or equivalent) consistent with the quickstarts and Turn 0 test destination.

## Success criteria

**Measurement:** Heuristic `scoreScenario05` in `[src/score-transcript.ts](../src/score-transcript.ts)`; LLM judge maps the bullets below (`[README.md` § Measuring scenarios](../README.md#measuring-scenarios)). Execution row is manual.

- Next.js project structure with install/run instructions.
- API routes or server actions perform Outpost calls; **no API key** in client bundles.
- UI flow covers **create destination** and **publish** (two distinct actions visible to the user).
- Tenant id and topic are configurable or clearly documented constants.
- Uses managed base URL by default.
- README lists required env vars.
- **Execution (full pass):** After `npm install` and `npm run dev` (or documented command), a manual smoke test completes **both** flows: register webhook destination and trigger test publish, without 5xx from your app’s Outpost calls and with Outpost accepting the requests. Requires `OUTPOST_API_KEY` and related env in `.env.local` or as documented. _Skip only for transcript-only triage._

## Failure modes to note

- Calling Outpost directly from browser-side code with embedded key.
- Only publishing without a UI path to register the destination first.
- Hard-coding localhost Outpost without user request.
