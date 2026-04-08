# Scenario 8 — Integrate Outpost into an existing Next.js SaaS app

## Intent

Operators often have a **production-shaped SaaS codebase** (auth, teams, dashboard) and need **outbound webhooks** for their customers. This scenario measures whether the agent can **clone a known open-source baseline**, understand where **domain events** happen, and **wire Hookdeck Outpost** so events are **published** to Outpost (with **per-tenant webhook destinations** documented or implemented).

**Baseline application (pin this in evals):** [**leerob/next-saas-starter**](https://github.com/leerob/next-saas-starter) — Next.js, PostgreSQL, Drizzle, team/member flows, MIT license. It is a common reference for “real” SaaS structure; adjust the prompt if you standardize on another repo.

## Preconditions

- Node 18+; `git` available.
- Same Turn 0 placeholders as other scenarios (`OUTPOST_API_KEY` **not** in the prompt text; test destination URL from dashboard).

## Automated eval (Claude Agent SDK)

The harness **`cwd`** is an empty directory under `results/runs/<stamp>-scenario-08/`. The agent should **`git clone`** the baseline into that workspace (or a subdirectory), **`npm` / `pnpm install`** via **Bash**, then **Write** / **Edit** integration code. Reviewers inspect the run folder and transcript.

## Conversation script

### Turn 0

Paste the **## Template** block from [`hookdeck-outpost-agent-prompt.mdx`](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx), with `{{…}}` filled using your project or [`fixtures/placeholder-values-for-turn0.md`](../fixtures/placeholder-values-for-turn0.md).

### Turn 1 — User

> Option 3 — I’m not starting from scratch. Please clone **`https://github.com/leerob/next-saas-starter`** here, install it, and get it runnable. Then wire in **Hookdeck Outpost** so we can send **outbound webhooks** to our customers.
>
> I need this tied to **something real in the app** (not a throwaway demo page), and I need to understand how each customer gets their webhook registered. Put whatever I need to configure in the README (env vars, etc.). Keep secrets on the server only.

### Turn 2 — User (optional)

> When should we create or sync the Outpost **tenant** with our own customer or team model?

## Success criteria

**Measurement:** Heuristic `scoreScenario08` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge maps the bullets below ([`README.md` § Measuring scenarios](../README.md#measuring-scenarios)). Execution row is manual.

- Baseline app is the documented **next-saas-starter** (or an explicitly justified fork) with clone + install steps reflected in the transcript or run directory.
- **Outpost TypeScript SDK** used **server-side only**; no `NEXT_PUBLIC_*` API key.
- At least one **publish** (or equivalent) tied to a **real code path** in the baseline (not dead code).
- **Topic** aligns with Turn 0 configuration or is clearly named and documented.
- **Per-customer webhook** story is explained: destination creation / subscription to topic.
- README (or equivalent) lists **env vars** for Outpost.
- **Execution (full pass):** With `OUTPOST_API_KEY` set, the app runs; a manual path triggers the integrated publish and Outpost accepts the request (2xx/202 as appropriate). *Skip only for transcript-only triage.*

## Failure modes to note

- Pasting a greenfield Next app instead of integrating the **cloned** baseline.
- Publishing only from a demo route unrelated to the product model.
- Calling Outpost from client components with secrets.

## Future baselines

Java / .NET “existing app” scenarios can follow the same shape: fixed public baseline repo + Option 3 Turn 1 + Success criteria + `scoreScenarioNN`.
