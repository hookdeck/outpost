# Scenario 4 — Basics with Go

## Intent

Agent should produce a **small Go program** using `github.com/hookdeck/outpost/sdks/outpost-go`, equivalent to scenarios 2–3.

## Preconditions

- Go toolchain; module with `outpost-go` dependency.
- `OUTPOST_API_KEY`, `OUTPOST_TEST_WEBHOOK_URL` set.

## Automated eval (Claude Agent SDK)

The harness sets the agent **cwd** to `docs/agent-evaluation/results/runs/<stamp>-scenario-NN/`. Write `go.mod`, `main.go`, etc. with **Write** / **Edit**; use **Bash** for `go mod init`, `go mod tidy`, and `go run` so the folder is a complete module.

## Conversation script

### Turn 0

Paste the **## Template** block from [`hookdeck-outpost-agent-prompt.mdoc`](../../agent-evaluation/hookdeck-outpost-agent-prompt.md), with `{{…}}` filled using your project or [`fixtures/placeholder-values-for-turn0.md`](../fixtures/placeholder-values-for-turn0.md).

### Turn 1 — User

> Option 1. I want to try it in **Go**.

### Turn 2 — User (optional)

> Keep the program small — one `main` or a couple of files is fine.

## Success criteria

**Measurement:** Heuristic `scoreScenario04` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge maps the checklist below ([`README.md` § Measuring scenarios](../README.md#measuring-scenarios)). Execution row is manual.

- [ ] `outpostgo.New` with `WithSecurity` (and optional `WithServerURL`).
- [ ] `Tenants.Upsert`, `Destinations.Create` with `CreateDestinationCreateWebhook` (or correct union wrapper), `Publish.Event`.
- [ ] Topic and tenant id explicit; matches prompt topics.
- [ ] No API key in source.
- [ ] **Execution (full pass):** With `OUTPOST_API_KEY`, `OUTPOST_TEST_WEBHOOK_URL`, and optional server URL env vars set, `go run …` succeeds and prints ids or clear success. _Skip only for transcript-only triage._

## Failure modes to note

- Passing raw struct to `Create` without `CreateDestinationCreateWebhook` wrapper (common compile mistake).
