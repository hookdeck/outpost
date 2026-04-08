# Scenario 7 — Minimal example app (Go net/http)

## Intent

Same behavior as scenarios 5–6: **small Go program** using `net/http` (no heavy framework required) that serves **basic HTML** with:

1. Form or fields for webhook URL → create webhook destination (via `outpost-go`).
2. Control to **publish** one test event.

## Preconditions

- Go 1.22+; `outpost-go` module.

## Automated eval (Claude Agent SDK)

The harness sets the agent **cwd** to `docs/agent-evaluation/results/runs/<stamp>-scenario-NN/`. Initialize the module and server **there** (`go mod init`, `go get`, etc. via **Bash**; `main.go` / `handlers.go` via **Write** / **Edit`). Reviewers should be able to `go run .` from the run directory after the eval.

## Conversation script

### Turn 0

Paste the **## Template** block from `[hookdeck-outpost-agent-prompt.mdx](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx)`, with `{{…}}` filled using your project or `[fixtures/placeholder-values-for-turn0.md](../fixtures/placeholder-values-for-turn0.md)`.

### Turn 1 — User

> Option 2 — **Go** with the standard library: small HTTP server, basic HTML, register a webhook and publish one test event.

### Turn 2 — User (optional)

> One or two files is fine if you can keep it readable.

## Success criteria

**Measurement:** Heuristic `scoreScenario07` in [`src/score-transcript.ts`](../src/score-transcript.ts); LLM judge maps the bullets below ([`README.md` § Measuring scenarios](../README.md#measuring-scenarios)). Execution row is manual.

- `go run .` (or `go run main.go`) documented.
- HTML UI with two flows: **create destination**, **publish**.
- SDK used server-side only; `OUTPOST_API_KEY` from env.
- Correct `CreateDestinationCreateWebhook` usage.
- README lists env vars and port.
- **Execution (full pass):** `go run …` starts the server; manual smoke test completes **create destination** and **publish** through the HTML UI without Outpost API failures. `OUTPOST_API_KEY` (and related env) set. *Skip only for transcript-only triage.*

## Failure modes to note

- Embedding API key in HTML/JS.
- Omitting publish action after destination registration.