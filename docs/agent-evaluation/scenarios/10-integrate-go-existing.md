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

> Existing **Go** API—you’re in this repo with me. Get it building, then add **Hookdeck Outpost** for outbound webhooks.
>
> Trigger **publish** from **one real handler** (signup, billing, etc.—not a throwaway test-only route by itself). **`topic` values should match that domain**. If our Hookdeck project’s topic list is missing something, document what to add; don’t point production code at the wrong names just to match a stub list unless I’ve said this is a minimal wiring pass. **`OUTPOST_API_KEY`** from env only. Explain how customers register webhook URLs and what to put in **README** / env. Use the **test receiver URL** from our Hookdeck setup when you want to prove delivery end-to-end.

### Turn 2 — User (optional)

> If customers submit a webhook URL in a settings endpoint, where does destination creation live?

## Success criteria

**Measurement:** Heuristic `scoreScenario10` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge; execution manual.

- **startersaas-go-api** (or documented alternative) present via harness **`preSteps`** with build instructions attempted in the transcript or tree.
- **Outpost Go SDK** used with **`Publish.Event`** (and related types) on a **real** handler path—not only a test-only route unless wiring-only scope was agreed.
- No API key in source; **`os.Getenv("OUTPOST_API_KEY")`** (or config loader) only.
- **Topic reconciliation** (domain-first; operator adds missing Hookdeck topics as documented) + **destination** documentation for operators; **tenant** mapping consistent.
- **Execution (full pass):** Server runs; trigger the **domain** handler; Outpost accepts publish. *Skip for transcript-only.*

## Failure modes to note

- New `main.go` only, without using the **cloned** baseline’s routes/models.
- Wrong `Create` shape without **`CreateDestinationCreateWebhook`** when creating webhook destinations.
- Publish only from a **test** helper with no real handler path.
