# Scenario 10 — Integrate Outpost into an existing Go SaaS API

## Intent

Same integration goal as [scenarios 8–9](08-integrate-nextjs-existing.md), for a **Go** REST API baseline with **auth and typical SaaS** structure.

**Baseline application (pin this in evals):** [**devinterface/startersaas-go-api**](https://github.com/devinterface/startersaas-go-api) — Go API, JWT, MongoDB, Stripe hooks, Docker — MIT license, small enough to clone in an eval. If you standardize on another Go SaaS boilerplate, update this file and `scoreScenario10`’s baseline check.

## Preconditions

- Go 1.21+; `git` available.

## Automated eval (Claude Agent SDK)

**`cwd`** is `results/runs/<stamp>-scenario-10/`. Expect **`git clone`**, **`go mod`** / **`go get`** for **`outpost-go`**, then source edits.

## Conversation script

### Turn 0

Paste the **## Template** from [`hookdeck-outpost-agent-prompt.mdx`](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx) with placeholders filled.

### Turn 1 — User

> Option 3 — existing Go API. Clone **`https://github.com/devinterface/startersaas-go-api`**, get it building, then add **Hookdeck Outpost** for outbound webhooks.
>
> Use **one real handler** as the publish trigger (signup, billing, etc.). API key from env only. Document how customers register webhook URLs and what to set in env. Use the test destination from the dashboard prompt where it helps.

### Turn 2 — User (optional)

> If customers submit a webhook URL in a settings endpoint, where does destination creation live?

## Success criteria

**Measurement:** Heuristic `scoreScenario10` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge; execution manual.

- Cloned **startersaas-go-api** (or documented alternative) with build instructions attempted.
- **Outpost Go SDK** used with **`Publish.Event`** (and related types) on a **real** handler path.
- No API key in source; **`os.Getenv("OUTPOST_API_KEY")`** (or config loader) only.
- **Topic** + **destination** documentation for operators.
- **Execution (full pass):** Server runs; trigger handler; Outpost accepts publish. *Skip for transcript-only.*

## Failure modes to note

- New `main.go` only, without using the **cloned** baseline’s routes/models.
- Wrong `Create` shape without **`CreateDestinationCreateWebhook`** when creating webhook destinations.
