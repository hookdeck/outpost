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

> **Option 3 — integrate with an existing app.** Clone **`https://github.com/devinterface/startersaas-go-api`** into this workspace and make it build (`go build` / `go test` ./… as appropriate per the repo).
>
> Add **Hookdeck Outpost** for **outbound webhooks** to customers:
>
> 1. Use the official **Go SDK** (`github.com/hookdeck/outpost/sdks/outpost-go` or current module path from docs).
> 2. **`OUTPOST_API_KEY`** from environment only.
> 3. On **one real domain event** in this API (e.g. user registration, subscription, or another existing handler), call **`Publish.Event`** (and **`Tenants` / `Destinations`** as needed) with a **topic** from Turn 0.
> 4. Document how to register **webhook destinations** per tenant and which env vars to set. Mention the Hookdeck test destination URL from Turn 0 where useful.

### Turn 2 — User (optional)

> Show where **`CreateDestinationCreateWebhook`** fits if we let each customer paste a webhook URL in a settings API.

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
