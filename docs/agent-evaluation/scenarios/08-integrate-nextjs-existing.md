# Scenario 8 — Integrate Outpost into an existing Next.js SaaS app

## Intent

Operators often have a **production-shaped SaaS codebase** (auth, teams, dashboard) and need **outbound webhooks** for their customers. This scenario measures whether the agent can work **inside an existing app tree** (here: a pinned open-source baseline), understand where **domain events** happen, and **wire Hookdeck Outpost** so events are **published** to Outpost (with **per-tenant webhook destinations** documented or implemented).

**Baseline application (pin this in evals):** [**leerob/next-saas-starter**](https://github.com/leerob/next-saas-starter) — Next.js, PostgreSQL, Drizzle, team/member flows, MIT license. It is a common reference for “real” SaaS structure; adjust the prompt if you standardize on another repo.

## Preconditions

- Node 18+; `git` available.
- Same **initial onboarding prompt** as other scenarios (`OUTPOST_API_KEY` **not** in the pasted text; test destination URL from dashboard).

## Eval harness

The runner executes **`preSteps`** below with shell **`cwd`** = `results/runs/<stamp>-scenario-08/` before Turn 0. **`agentCwd`** is the SDK process working directory (the baseline repo root). Set **`EVAL_SKIP_HARNESS_PRE_STEPS=1`** to skip preSteps; if **`agentCwd`** is missing, the harness falls back to the run directory. When **`urlEnv`** is set and that variable is non-empty, it overrides **`url`**.

```eval-harness
{
  "preSteps": [
    {
      "type": "git_clone",
      "url": "https://github.com/leerob/next-saas-starter.git",
      "into": "next-saas-starter",
      "depth": 1,
      "urlEnv": "EVAL_NEXT_SAAS_BASELINE_URL"
    }
  ],
  "agentCwd": "next-saas-starter"
}
```

## Automated eval (Claude Agent SDK)

Same as other scenarios, except the agent starts **inside** the cloned tree above. Expect **`npm` / `pnpm install`** via **Bash**, then **Write** / **Edit** for Outpost. Reviewers inspect that tree plus `transcript.json`.

## Conversation script

### Turn 0

Paste the **## Template** block from [`hookdeck-outpost-agent-prompt.mdoc`](../../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc), with `{{…}}` filled using your project or [`fixtures/placeholder-values-for-turn0.md`](../fixtures/placeholder-values-for-turn0.md).

### Turn 1 — User

> I’m integrating into our existing **Next.js** SaaS app—you’re in this repo with me. Install dependencies, get it running, then add **Hookdeck Outpost** so we can send **outbound webhooks** to our customers.
>
> Tie it to **real product behavior** (not a throwaway demo page). I need a clear story for **how each customer registers their webhook** and which topics they receive. Use **topic names that match our domain**; if Hookdeck doesn’t list a topic we need yet, tell me exactly what to add in the project—don’t point our code at the wrong names just to match a short list unless I’ve said we’re only doing a quick wiring spike. Document env vars and setup in the **README**. Keep the Outpost API key on the **server** only.

### Turn 2 — User (optional)

> When should we create or sync the Outpost **tenant** with our own customer or team model?

## Success criteria

**Measurement:** Heuristic `scoreScenario08` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge maps the bullets below ([`README.md` § Measuring scenarios](../README.md#measuring-scenarios)). Execution row is manual.

**Contract:** The baseline ships a **customer-facing dashboard**. Treat it like **Existing application (full-stack products)** in [`hookdeck-outpost-agent-prompt.mdoc`](../../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc). The detailed UI bar is **not** repeated here—use **[Building your own UI — Implementation checklists](../../content/guides/building-your-own-ui.mdoc#implementation-checklists)** (*Planning and contract*, *Destinations experience*, *Activity, attempts, and retries*). The agent must self-verify with **Before you stop (verify)** in the same prompt (full-stack UI item).

- Baseline app is the documented **next-saas-starter** (or an explicitly justified fork): harness clone under the run directory plus install / integration steps reflected in the transcript or that tree.
- **Outpost TypeScript SDK** used **server-side only**; no `NEXT_PUBLIC_*` API key.
- **Topic reconciliation:** README or inline notes map **each `publish` topic** to a **real domain event**; if the app needs topics not in the **configured project list** from onboarding, instructions say to **add them in Hookdeck** (domain-first—not reshaping product logic to fit a stale default list unless wiring-only scope was agreed).
- **Domain publish:** At least one **`publish` on a real domain path** (signup, CRUD, billing, etc.)—**not** only a synthetic “test event” route.
- **Separate test publish:** A **distinct** server-side control (button, action, or route) that publishes a **test** event for the signed-in tenant—**in addition to** domain publish; does **not** satisfy the domain-publish requirement by itself (see prompt).
- **Full-stack destination + activity UI:** Customers can **drill into** a destination (detail or edit—per product policy), reach **destination-scoped activity** (events / attempts / manual retry for failures) via **your** authenticated routes, and **create** destinations using **dynamic** fields from **`GET /destination-types`** (each field’s **`key`** → `config` / `credentials`). **List rows** link or navigate into that flow—not **only** create + delete with no detail or activity. Omit sub-items only if Turn 1 explicitly scoped **backend-only** or excluded activity UI (then document how operators verify delivery instead).
- **Per-customer webhook** story: **tenant ↔ customer** mapping is consistent for publish and destination APIs.
- README (or equivalent) lists **env vars** for Outpost.
- **Execution (full pass):** With `OUTPOST_API_KEY` set, the app runs; perform a **real in-app action** that triggers the domain publish and confirm Outpost accepts it (2xx/202). Exercise **test publish** and **activity / retry** in the UI when present. Smoke from **`results/runs/…-scenario-08/next-saas-starter/`** (not transcript-only triage).

## Failure modes to note

- Pasting a greenfield Next app instead of integrating the **baseline** in the workspace.
- **List-only** destinations (no drill-down to detail or destination-scoped activity) while the baseline still has a product dashboard—unless the user explicitly scoped backend-only.
- **No separate test publish** when customers can manage destinations from the UI.
- Publishing only from a demo or **test-only** route with no domain path.
- **Topics** in code with no README telling the operator to **add** them in Hookdeck when the onboarding topic list was incomplete (or silently retargeting domain logic to unrelated configured names).
- Calling Outpost from client components with secrets.

## Future baselines

Java / .NET “existing app” scenarios can follow the same shape: harness pre-clones a fixed public baseline into the run workspace + a natural-language **integration** Turn 1 + Success criteria + `scoreScenarioNN`.
