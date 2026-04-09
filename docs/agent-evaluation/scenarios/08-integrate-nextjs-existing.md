# Scenario 8 — Integrate Outpost into an existing Next.js SaaS app

## Intent

Operators often have a **production-shaped SaaS codebase** (auth, teams, dashboard) and need **outbound webhooks** for their customers. This scenario measures whether the agent can work **inside an existing app tree** (here: a pinned open-source baseline), understand where **domain events** happen, and **wire Hookdeck Outpost** so events are **published** to Outpost (with **per-tenant webhook destinations** documented or implemented).

**Baseline application (pin this in evals):** [**leerob/next-saas-starter**](https://github.com/leerob/next-saas-starter) — Next.js, PostgreSQL, Drizzle, team/member flows, MIT license. It is a common reference for “real” SaaS structure; adjust the prompt if you standardize on another repo.

## Preconditions

- Node 18+; `git` available.
- Same Turn 0 placeholders as other scenarios (`OUTPOST_API_KEY` **not** in the prompt text; test destination URL from dashboard).

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

Paste the **## Template** block from [`hookdeck-outpost-agent-prompt.mdx`](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx), with `{{…}}` filled using your project or [`fixtures/placeholder-values-for-turn0.md`](../fixtures/placeholder-values-for-turn0.md).

### Turn 1 — User

> Option 3 — I’m not starting from scratch. **We’re already in the Next.js SaaS app in this workspace** — the baseline repo is checked out here. Install dependencies and get it runnable, then wire in **Hookdeck Outpost** so we can send **outbound webhooks** to our customers.
>
> I need this tied to **something real in the app** (not a throwaway demo page), and I need to understand how each customer gets their webhook registered. Put whatever I need to configure in the README (env vars, etc.). Keep secrets on the server only.

### Turn 2 — User (optional)

> When should we create or sync the Outpost **tenant** with our own customer or team model?

## Success criteria

**Measurement:** Heuristic `scoreScenario08` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge maps the bullets below ([`README.md` § Measuring scenarios](../README.md#measuring-scenarios)). Execution row is manual.

- Baseline app is the documented **next-saas-starter** (or an explicitly justified fork): harness clone under the run directory plus install / integration steps reflected in the transcript or that tree.
- **Outpost TypeScript SDK** used **server-side only**; no `NEXT_PUBLIC_*` API key.
- At least one **publish** (or equivalent) tied to a **real code path** in the baseline (not dead code).
- **Topic** aligns with Turn 0 configuration or is clearly named and documented.
- **Per-customer webhook** story is explained: destination creation / subscription to topic.
- README (or equivalent) lists **env vars** for Outpost.
- **Execution (full pass):** With `OUTPOST_API_KEY` set, the app runs; a manual path triggers the integrated publish and Outpost accepts the request (2xx/202 as appropriate). Run smoke tests from **`results/runs/…-scenario-08/next-saas-starter/`** (not transcript-only triage).

## Failure modes to note

- Pasting a greenfield Next app instead of integrating the **baseline** in the workspace.
- Publishing only from a demo route unrelated to the product model.
- Calling Outpost from client components with secrets.

## Future baselines

Java / .NET “existing app” scenarios can follow the same shape: harness pre-clones a fixed public baseline into the run workspace + Option 3 Turn 1 (user already “in” the app) + Success criteria + `scoreScenarioNN`.
