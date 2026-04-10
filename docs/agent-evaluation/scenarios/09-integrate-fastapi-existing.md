# Scenario 9 — Integrate Outpost into an existing FastAPI SaaS app

## Intent

Same as [scenario 8](08-integrate-nextjs-existing.md), but stack is **Python + FastAPI** with a **multi-tenant / team** style baseline that also ships a **real web UI** (so operators can exercise dashboards, not only OpenAPI).

**Baseline application (pin this in evals):** [**fastapi/full-stack-fastapi-template**](https://github.com/fastapi/full-stack-fastapi-template) — maintained full-stack app: **FastAPI** backend (SQLModel, **Pydantic v2**), **React + TypeScript + Vite** frontend, PostgreSQL, Docker Compose, JWT auth, MIT license. Substitute only if you document another baseline in the scenario and update heuristics.

**Supersedes:** The previous pin [**philipokiokio/FastAPI_SAAS_Template**](https://github.com/philipokiokio/FastAPI_SAAS_Template) (stale dependencies, API-only, no product UI).

## Preconditions

- Python 3.10+; **Node.js 18+** (for the frontend); `git` available.
- **Docker** (recommended) — template dev flow uses Docker Compose for API, DB, and frontend; see repository `development.md`.
- Same Turn 0 placeholders as other scenarios (`OUTPOST_API_KEY` **not** in the prompt text; test destination URL from dashboard).

## Eval harness

```eval-harness
{
  "preSteps": [
    {
      "type": "git_clone",
      "url": "https://github.com/fastapi/full-stack-fastapi-template.git",
      "into": "full-stack-fastapi-template",
      "depth": 1,
      "urlEnv": "EVAL_FASTAPI_BASELINE_URL"
    }
  ],
  "agentCwd": "full-stack-fastapi-template"
}
```

Optional: set **`EVAL_FASTAPI_BASELINE_URL`** to override the clone URL (fork or pinned commit).

## Automated eval (Claude Agent SDK)

The agent starts **inside** the cloned baseline above. Expect **`docker compose`** and/or **`uv` / `pip`** per **`development.md`** and **`backend/README.md`**, then **Write** / **Edit** for Outpost integration (backend-first; UI hooks optional but encouraged when they clarify the customer webhook story).

## Conversation script

### Turn 0

Paste the **## Template** from [`hookdeck-outpost-agent-prompt.mdx`](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx) with placeholders filled.

### Turn 1 — User

> Option 3 — integrate Outpost into a real codebase. **We’re already in the full-stack FastAPI template in this workspace** — the repository is present here. Follow the project’s dev docs to get backend (and frontend if useful) running, then add **Hookdeck Outpost** for customer webhooks.
>
> Hook publishing to **one real event** that already exists in the app (users, items, teams, whatever fits). **Topic strings should match that domain**; if Turn 0’s list doesn’t include the right names yet, document what the operator must **add in the Outpost project**—don’t contort the app to arbitrary topics unless this is explicitly a minimal wiring pass. Document topics, how tenants register webhook URLs, and env vars. Don’t leak the API key to the client.

### Turn 2 — User (optional)

> When should we create or sync the Outpost **tenant** with our own customer or team model?

## Success criteria

**Measurement:** Heuristic `scoreScenario09` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge; execution manual.

- **full-stack-fastapi-template** (or documented alternative) present via harness **`preSteps`** with install steps in the transcript or tree.
- **`outpost_sdk`** with **`publish.event`** (and related calls as needed) on a **real** code path in the **backend** (server-side only for secrets)—**not** only a synthetic test-publish endpoint unless the scenario was explicitly scoped to wiring-only.
- API key from **environment** or secure settings — not hard-coded or exposed to clients.
- **Topic reconciliation:** each **`topic` in code** ties to a real domain event; gaps vs Turn 0 are resolved by **operator adding topics in Hookdeck** (documented), not by retargeting domain logic to a mismatched list unless wiring-only scope was agreed.
- **Destination** story documented; if the app has a UI, linking or exposing **safe** controls for customer destinations is a plus; **tenant id** usage consistent with publish.
- README (or equivalent) lists **env vars** for Outpost.
- **Execution (full pass):** Stack runs per template docs; trigger a **real domain action** that fires publish; Outpost accepts. A test-publish button may be used **additionally** for smoke. *Skip for transcript-only.*

## Failure modes to note

- Greenfield FastAPI “hello world” instead of the **cloned** baseline.
- Using raw `httpx` to Outpost when the scenario asks for **`outpost_sdk`**.
- Putting `OUTPOST_API_KEY` in `NEXT_PUBLIC_*` / client bundles.
- **Only** test/synthetic publish with no domain hook.

## Future baselines

Other “existing FastAPI app” pins can follow the same shape: harness pre-clone + Option 3 Turn 1 + success criteria + `scoreScenario09`.
