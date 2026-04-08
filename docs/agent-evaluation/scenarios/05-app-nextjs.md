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

Paste the **## Template** block from `[hookdeck-outpost-agent-prompt.mdx](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx)`, with `{{…}}` filled using your project or `[fixtures/placeholder-values-for-turn0.md](../fixtures/placeholder-values-for-turn0.md)`.

### Turn 1 — User

> Option 2 — build a minimal example. I want **Next.js**. Very small UI: field for webhook URL, button to create the webhook destination for tenant `demo_tenant` (or let me edit tenant id in the UI), and a button to send one test event on topic `user.created` (or the first topic from the prompt). Use the Outpost TypeScript SDK on the server only.

### Turn 2 — User (optional)

> Add a short README with env vars and `npm run dev` steps.

### Turn 3 — User (stress)

> I do not have a public URL yet — what should I use for the webhook URL field?

Expected: agent suggests Hookdeck Console Source URL or similar, aligned with quickstarts.

## Success criteria

**Measurement:** Heuristic `scoreScenario05` in `[src/score-transcript.ts](../src/score-transcript.ts)`; LLM judge maps the bullets below (`[README.md` § Measuring scenarios](../README.md#measuring-scenarios)). Execution row is manual.

- Next.js project structure with install/run instructions.
- API routes or server actions perform Outpost calls; **no API key** in client bundles.
- UI flow covers **create destination** and **publish** (two distinct actions visible to the user).
- Tenant id and topic are configurable or clearly documented constants.
- Uses managed base URL by default.
- README lists required env vars.
- **Execution (full pass):** After `npm install` and `npm run dev` (or documented command), a manual smoke test completes **both** flows: register webhook destination and trigger test publish, without 5xx from your app’s Outpost calls and with Outpost accepting the requests. Requires `OUTPOST_API_KEY` and related env in `.env.local` or as documented. *Skip only for transcript-only triage.*

## Failure modes to note

- Calling Outpost directly from browser-side code with embedded key.
- Only publishing without a UI path to register the destination first.
- Hard-coding localhost Outpost without user request.

