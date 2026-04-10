# Scenario 9 — Integrate Outpost into an existing FastAPI SaaS app

## Intent

Same as [scenario 8](08-integrate-nextjs-existing.md), but stack is **Python + FastAPI** with a **multi-tenant / team** style baseline that also ships a **real web UI** (so operators can exercise dashboards, not only OpenAPI).

**Baseline application (pin this in evals):** [**fastapi/full-stack-fastapi-template**](https://github.com/fastapi/full-stack-fastapi-template) — maintained full-stack app: **FastAPI** backend (SQLModel, **Pydantic v2**), **React + TypeScript + Vite** frontend, PostgreSQL, Docker Compose, JWT auth, MIT license. Substitute only if you document another baseline in the scenario and update heuristics.

**Supersedes:** The previous pin [**philipokiokio/FastAPI_SAAS_Template**](https://github.com/philipokiokio/FastAPI_SAAS_Template) (stale dependencies, API-only, no product UI).

## Preconditions

- Python 3.10+; **Node.js 18+** (for the frontend); `git` available.
- **Docker** (recommended) — template dev flow uses Docker Compose for API, DB, and frontend; see repository `development.md`.
- Same **initial onboarding prompt** as other scenarios (`OUTPOST_API_KEY` **not** in the pasted text; test destination URL from dashboard).

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

Paste the **## Template** from [`hookdeck-outpost-agent-prompt.mdoc`](../../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc) with placeholders filled.

### Turn 1 — User

> This workspace is our **full-stack FastAPI + React** product (the template we ship). Follow the repo’s dev docs to bring up API, DB, and frontend, then integrate **Hookdeck Outpost** for **per-customer webhooks**.
>
> I want customers to manage **destinations** from the product (or through our authenticated API), a **separate** way to **fire a test event** that isn’t pretending to be production traffic, and enough **delivery visibility** that they can see **events**, **attempts**, and **retry** when something failed—all **through our backend**, never with the platform API key in the browser.
>
> Wire **publish** into **one real workflow** we already have (signups, records, teams—whatever fits this codebase). **Topics** should match that workflow. If Hookdeck doesn’t list a name we need, document what I should add there; don’t reshape the product around random topic strings unless I’ve said this is wiring-only. Document env vars and how **tenant** maps to our customer or team model. Don’t expose the API key to clients.

### Turn 2 — User (optional)

> When should we create or sync the Outpost **tenant** with our own customer or team model?

## Success criteria

**Measurement:** Heuristic `scoreScenario09` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge (reads this section); execution manual.

**Contract:** Same full-stack bar as scenario **8**, pinned to this template. **Canonical checklist:** [Building your own UI — Implementation checklists](../../content/guides/building-your-own-ui.mdoc#implementation-checklists). **Agent self-verify:** [`hookdeck-outpost-agent-prompt.mdoc`](../../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc) → *Before you stop (verify)* (full-stack UI item). Do not duplicate checklist rows in transcripts—confirm against the guide.

- **full-stack-fastapi-template** (or documented alternative) present via harness **`preSteps`** with install steps in the transcript or tree.
- **`outpost_sdk`** with **`publish.event`** (and related calls as needed) on a **real** code path in the **backend** (server-side only for secrets)—**not** only a synthetic test-publish endpoint unless the scenario was explicitly scoped to wiring-only.
- **Domain + test publish:** At least one **`publish` on a real domain path** (entity create/update, signup, etc.). A **separate** test-publish path or control is **required** for this baseline—it **does not** replace the domain publish requirement.
- API key from **environment** or secure backend settings only — not hard-coded, not exposed via **`NEXT_PUBLIC_*`**, **`VITE_*`**, or other client-visible env patterns.
- **Topic reconciliation:** each **`topic` in code** ties to a real domain event; gaps vs the **configured project topic list** from onboarding are resolved by **adding topics in Hookdeck** (documented), not by retargeting domain logic to a mismatched list unless wiring-only scope was agreed.
- **Destinations + tenant:** Per-customer (or per-team) **destination** management via **authenticated** UI or BFF routes: **list**, **create**, and **drill-down** (detail and **destination-scoped activity**—events, attempts, **manual retry**). **Dynamic** forms from **`GET /destination-types`** with correct **`key`** → `config` / `credentials`. **`tenant_id`** is consistent between publish and destination APIs. Omit drill-down / activity only if Turn 1 scoped **backend-only** or excluded activity UI (document verification instead).
- **Operator docs:** Root **README**, **backend/README**, **development.md**, or **`.env.example`** (whichever the template uses) lists **Outpost env vars** and how to run and verify.
- **Execution (full pass):** Stack runs per template docs; trigger a **real domain action** that fires publish; Outpost accepts. Exercise **test publish** and **activity / retry** in the UI when in scope. *Skip for transcript-only.*

## Failure modes to note

- Greenfield FastAPI “hello world” instead of the **cloned** baseline.
- Using raw `httpx` to Outpost when the scenario asks for **`outpost_sdk`**.
- Putting `OUTPOST_API_KEY` in `NEXT_PUBLIC_*`, `VITE_*`, or other client bundles.
- **Only** test/synthetic publish with no domain hook, or **only** domain publish with no **separate** test-publish control when a dashboard is in scope.
- **No** events/attempts/retry surfaced for customers when the baseline includes a product UI and the user did not ask to skip that scope.
- **Flat list** of destinations with no navigation to **detail** or **per-destination activity** (same as scenario 8 failure mode).

## Future baselines

Other “existing FastAPI app” pins can follow the same shape: harness pre-clone + natural-language integration Turn 1 + success criteria + `scoreScenario09`.
