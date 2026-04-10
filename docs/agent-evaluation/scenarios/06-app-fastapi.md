# Scenario 6 — Minimal example app (FastAPI + Jinja or HTMX)

## Intent

Same product behavior as [scenario 5](05-app-nextjs.md), but stack is **Python FastAPI**:

- Server renders a **simple HTML form** (Jinja2 templates, HTMX, or minimal static HTML served by FastAPI).
- Endpoints (or form posts) call `outpost_sdk` with env-based API key.
- User can submit webhook URL → create destination; user can trigger test publish.

## Preconditions

- Python 3.9+; `fastapi`, `uvicorn`, `outpost_sdk`.

## Automated eval (Claude Agent SDK)

The harness sets the agent **cwd** to `docs/agent-evaluation/results/runs/<stamp>-scenario-NN/`. Create the FastAPI app **in that directory**: add source files with **Write** / **Edit**, install deps with **Bash** (`pip` / `uv`). The run folder must be a small but complete app (not only code pasted in chat).

## Conversation script

### Turn 0

Paste the **## Template** block from [`hookdeck-outpost-agent-prompt.mdoc`](../../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc), with `{{…}}` filled using your project or [`fixtures/placeholder-values-for-turn0.md`](../fixtures/placeholder-values-for-turn0.md).

### Turn 1 — User

> Option 2 — **FastAPI**, same idea as a tiny demo: simple HTML, register a webhook for a tenant, button to send one test event. Keep the codebase small.

### Turn 2 — User (optional)

> README with env vars and how to run it would help.

## Success criteria

**Measurement:** Heuristic `scoreScenario06` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge maps the checklist below ([`README.md` § Measuring scenarios](../README.md#measuring-scenarios)). Execution row is manual.

- [ ] FastAPI app runs with one command documented (`uvicorn ...`).
- [ ] Outpost calls only server-side; API key from environment.
- [ ] Two user-visible actions: **register webhook** and **publish test event**.
- [ ] Managed API base URL by default.
- [ ] README with `OUTPOST_API_KEY`, `OUTPOST_TEST_WEBHOOK_URL` or equivalent.
- [ ] **Execution (full pass):** App starts (`uvicorn` or as documented); manual smoke test completes **register webhook** and **publish test event** without server errors on Outpost calls. Env vars set including `OUTPOST_API_KEY`. *Skip only for transcript-only triage.*

## Failure modes to note

- Exposing API key to templates/inline JS.
- Using only `curl` subprocesses when user asked for FastAPI + SDK.
