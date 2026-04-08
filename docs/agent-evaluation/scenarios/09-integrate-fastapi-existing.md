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

> **Option 3 — integrate with an existing app.** Clone **`https://github.com/philipokiokio/FastAPI_SAAS_Template`** into this workspace, install dependencies per its README (venv + `pip install -r requirements.txt` or `uv` as you prefer).
>
> Integrate **Hookdeck Outpost** for **outbound webhooks**:
>
> 1. Use **`outpost_sdk`** only in **server** code (routers, services — never embed the API key in templates or static JS).
> 2. Hook **`publish.event`** (and tenant/destination setup as needed) to **one real domain event** in this template (e.g. org membership or user lifecycle — pick something that exists in the codebase).
> 3. Document how operators register **webhook destinations** per tenant/customer and which **topic** you publish on (use topics from Turn 0 when possible).
> 4. Document **`OUTPOST_API_KEY`** and **`uvicorn`** (or equivalent) run instructions in README.

### Turn 2 — User (optional)

> Should **`tenants.upsert`** run at org creation or lazily on first publish?

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
