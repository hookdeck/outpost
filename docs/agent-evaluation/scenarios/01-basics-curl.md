# Scenario 1 — Basics with curl

## Intent

Agent should produce a **minimal shell + curl** flow against the **managed** API (no SDK), matching the official curl quickstart. Prefer a **single runnable shell script** (e.g. `outpost-quickstart.sh`) that sets variables and runs all curls, so the operator can `chmod +x` and run once; inline copy-paste blocks are acceptable if the user asked only for “commands.”

## Preconditions

- `OUTPOST_API_KEY` set in the environment (user states this; agent must not ask for the raw key in chat).
- Topics include at least one topic used in the script (e.g. `user.created`).

## Automated eval (Claude Agent SDK)

The harness sets the agent **cwd** to an empty directory under `docs/agent-evaluation/results/runs/<stamp>-scenario-NN/`. **`Write` / `Edit` / `NotebookEdit` paths are enforced** to that directory only (absolute paths elsewhere are denied). Save the script as e.g. **`outpost-quickstart.sh`** in that folder (relative path or a path under the run dir), not under `examples/` or the repo root.

## Conversation script

### Turn 0

Paste the **## Template** block from `[hookdeck-outpost-agent-prompt.mdx](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx)`, with `{{…}}` filled using your project or `[fixtures/placeholder-values-for-turn0.md](../fixtures/placeholder-values-for-turn0.md)`.

### Turn 1 — User

> I want option 1 — **the simplest thing possible**. I don’t need a framework or SDK; just the smallest path to see tenant → webhook → publish working.

### Turn 2 — User (optional)

> How do I know the event actually reached my test URL?

## Success criteria

**Measurement:** Heuristic rubric `scoreScenario01` in `[../src/score-transcript.ts](../src/score-transcript.ts)` (assistant text + tool-written script content). LLM judge: `npm run score -- --run <run-dir> --llm`. Execution row remains manual.

- Uses managed base URL `https://api.outpost.hookdeck.com/2025-07-01` (or explicit `OUTPOST_API_BASE_URL`), **not** `localhost:3333/api/v1`, unless the user asked for self-hosted.
- Tenant: `PUT .../tenants/{tenant_id}` with `Authorization: Bearer` (or documents equivalent).
- Destination: `POST .../tenants/{tenant_id}/destinations` with `type: webhook`, `topics` including the configured topic or `*`, and `config.url` pointing at a test HTTPS URL (env or placeholder).
- Publish: `POST .../publish` with `tenant_id`, `topic`, and a top-level JSON field `**data`** (the event payload object — see OpenAPI `PublishRequest` and curl quickstart). Not `payload`. Typically also `eligible_for_retry`.
- Delivers as one **shell script** (or one fenced `bash` block meant to be saved as `.sh`), not only three unrelated snippets without a shebang/variables.
- Does **not** embed a pasted API key in the reply.
- Verification mentions Hookdeck Console / dashboard logs if Turn 2 was asked.
- **Execution (full pass):** With `OUTPOST_API_KEY` (and `OUTPOST_API_BASE_URL` if the snippet uses it) set in your environment, run the agent’s tenant → destination → publish sequence against a real project. Expect **2xx** on tenant upsert and destination create, **202** (or documented success) on publish, and a visible delivery to the test webhook URL (Hookdeck Console / project logs, or `GET .../attempts` as appropriate). *Skip only if you are doing transcript-only triage.*

## Failure modes to note

- Wrong path (`PUT /{tenant}` without `/tenants/`).
- Mixing self-hosted base path with managed host.
- Skipping topic alignment with dashboard configuration.