# Scenario 10 — Integrate Outpost into an existing Go SaaS API

## Intent

Same integration goal as [scenarios 8–9](08-integrate-nextjs-existing.md), for a **Go** REST API baseline with **auth and typical SaaS** structure.

**Baseline application (pin this in evals):** [**devinterface/startersaas-go-api**](https://github.com/devinterface/startersaas-go-api) — Go API, JWT, MongoDB, Stripe hooks, Docker — MIT license, small enough to clone in an eval. If you standardize on another Go SaaS boilerplate, update this file and `scoreScenario10`’s baseline check.

## Preconditions

- Go 1.21+; `git` available.

## Eval harness

```eval-harness
{
  "preSteps": [
    {
      "type": "git_clone",
      "url": "https://github.com/devinterface/startersaas-go-api.git",
      "into": "startersaas-go-api",
      "depth": 1,
      "urlEnv": "EVAL_GO_SAAS_BASELINE_URL"
    }
  ],
  "agentCwd": "startersaas-go-api"
}
```

## Automated eval (Claude Agent SDK)

The agent starts **inside** the cloned baseline above. Expect **`go mod`** / **`go get`** for **`outpost-go`**, then source edits.

## Conversation script

### Turn 0

Paste the **## Template** from [`hookdeck-outpost-agent-prompt.mdx`](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx) with placeholders filled.

### Turn 1 — User

> Option 3 — existing Go API. **We’re already in the startersaas-go-api tree in this workspace** — the repository is present here. Get it building, then add **Hookdeck Outpost** for outbound webhooks.
>
> Use **one real handler** as the publish trigger (signup, billing, etc.). API key from env only. Document how customers register webhook URLs and what to set in env. Use the test destination from the dashboard prompt where it helps.

### Turn 2 — User (optional)

> If customers submit a webhook URL in a settings endpoint, where does destination creation live?

## Success criteria

**Measurement:** Heuristic `scoreScenario10` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge; execution manual.

- **startersaas-go-api** (or documented alternative) present via harness **`preSteps`** with build instructions attempted in the transcript or tree.
- **Outpost Go SDK** used with **`Publish.Event`** (and related types) on a **real** handler path.
- No API key in source; **`os.Getenv("OUTPOST_API_KEY")`** (or config loader) only.
- **Topic** + **destination** documentation for operators.
- **Execution (full pass):** Server runs; trigger handler; Outpost accepts publish. *Skip for transcript-only.*

## Failure modes to note

- New `main.go` only, without using the **cloned** baseline’s routes/models.
- Wrong `Create` shape without **`CreateDestinationCreateWebhook`** when creating webhook destinations.
