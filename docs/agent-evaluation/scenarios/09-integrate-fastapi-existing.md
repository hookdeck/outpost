# Scenario 9 — Integrate Outpost into an existing FastAPI SaaS app

## Intent

Same as [scenario 8](08-integrate-nextjs-existing.md), but stack is **Python + FastAPI** with a **multi-tenant / org** style baseline.

**Baseline application (pin this in evals):** [**philipokiokio/FastAPI_SAAS_Template**](https://github.com/philipokiokio/FastAPI_SAAS_Template) — FastAPI, organizations, permissions, Alembic, MIT-style OSS template commonly used as a starting point. Substitute only if you document another baseline in the scenario and update heuristics.

## Preconditions

- Python 3.10+; `git` available.

## Automated eval (Claude Agent SDK)

**`cwd`** is `results/runs/<stamp>-scenario-09/`. Expect **`git clone`**, **`pip` / `uv`**, then **Write** / **Edit** for Outpost integration.

## Conversation script

### Turn 0

Paste the **## Template** from [`hookdeck-outpost-agent-prompt.mdx`](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx) with placeholders filled.

### Turn 1 — User

> Option 3 — integrate Outpost into a real codebase. Clone **`https://github.com/philipokiokio/FastAPI_SAAS_Template`**, set it up from its README, then add **Hookdeck Outpost** for customer webhooks.
>
> Hook publishing to **one real event** that already exists in the app (orgs, users, whatever fits). Document topics, how tenants register webhook URLs, and env vars. Don’t leak the API key to the client.

### Turn 2 — User (optional)

> Should we create the Outpost tenant when the org is created, or lazily on first publish?

## Success criteria

**Measurement:** Heuristic `scoreScenario09` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge; execution manual.

- Cloned **FastAPI_SAAS_Template** (or documented alternative) with install steps.
- **`outpost_sdk`** with **`publish.event`** (and related calls as needed) on a **real** code path.
- API key from **environment** or secure settings — not hard-coded or exposed to clients.
- **Topic** and **destination** story documented.
- README updated for env + run.
- **Execution (full pass):** App starts; trigger path fires publish; Outpost accepts. *Skip for transcript-only.*

## Failure modes to note

- Greenfield FastAPI “hello world” instead of the **cloned** template.
- Using raw `httpx` to Outpost when the scenario asks for **`outpost_sdk`**.
