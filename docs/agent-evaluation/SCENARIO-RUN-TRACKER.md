# Scenario run tracker

Use this table while you **run scenarios one at a time** and **execute the generated artifacts** against a real Outpost project.

## How to use

1. **Automated agent eval** (from `docs/agent-evaluation/`):
  ```sh
   npm run eval -- --scenario <NN>
  ```
   Each run creates `**results/runs/<ISO-stamp>-scenario-<NN>/**` with `transcript.json`, `heuristic-score.json`, `llm-score.json`, and whatever the agent wrote (scripts, apps, clones).
2. **Fill the table:** paste or note the **run directory** (stamp), mark **Heuristic** / **LLM** pass or fail (from the sidecars or console). **Run directory** should be the **latest** folder matching `results/runs/*-scenario-<NN>` whose `heuristic-score.json` has **`overallTranscriptPass: true`** (re-scan directories when updating this file).
3. **Execution (generated code):** with `**OUTPOST_API_KEY`** (and `**OUTPOST_TEST_WEBHOOK_URL`** / `**OUTPOST_API_BASE_URL`** if needed) in your shell or `.env`, run the artifact the scenario expects — e.g. `bash outpost-quickstart.sh`, `npx tsx …`, `python …`, `go run …`, `npm run dev` in the generated app folder. Mark **Pass** / **Fail** / **Skip** and add **Notes** (HTTP status, delivery in Hookdeck Console, etc.). **Do not edit generated files to force a pass** — test what the agent produced; note OS/environment (e.g. Linux vs macOS) when relevant. **This column is the primary bar for “does the output actually work?”** Heuristic and LLM scores are supplementary.
4. **Optional:** copy a row to your local run log under `results/` if you use `RUN-RECORDING.template.md`.

---

## Tracker


| ID  | Scenario file                                                                  | Run directory (`results/runs/…`)       | Heuristic              | LLM judge | Execution (generated code) | Notes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| --- | ------------------------------------------------------------------------------ | -------------------------------------- | ---------------------- | --------- | -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 01  | [01-basics-curl.md](scenarios/01-basics-curl.md)                               | `2026-04-10T09-28-52-764Z-scenario-01` | Pass (7/7)             | Pass      | Pass                       | Artifact: `**quickstart.sh`**. Heuristic + LLM from `npm run eval -- --scenario 01`; harness sidecars are sibling `*.eval-*.json` under `results/runs/` (not inside run dir). Execution: `OUTPOST_API_KEY` from `docs/agent-evaluation/.env` + `bash quickstart.sh` in run dir; tenant **200**, destination **201**, publish **202**; exit 0.                                                                                                                                                                                                                                     |
| 02  | [02-basics-typescript.md](scenarios/02-basics-typescript.md)                   | `2026-04-10T15-01-35-359Z-scenario-02` | Pass (9/9)             | Pass      | Pass                       | `EVAL_LOCAL_DOCS=1` after **scope-router** update to [agent prompt template](../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc). Artifact: `**outpost-quickstart.ts`** + `package.json` (SDK)—**no** Next.js scaffold. Heuristic + LLM pass; harness sidecars sibling under `results/runs/`. Earlier passes: `2026-04-10T10-49-02-890Z-scenario-02`, `2026-04-10T10-34-35-461Z-scenario-02`. Over-build run: `2026-04-10T09-39-06-362Z-scenario-02` (Next.js + script; LLM fail).                                                                                        |
| 03  | [03-basics-python.md](scenarios/03-basics-python.md)                           | `2026-04-10T11-02-19-073Z-scenario-03` | Pass (8/8)             | Pass      | Pass                       | `EVAL_LOCAL_DOCS=1` with [scope-router prompt](../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc). Artifact: `**outpost_quickstart.py`** + `.env.example` (`python-dotenv`, `outpost_sdk`)—**no** web framework. Heuristic + LLM pass; judge `execution_in_transcript` **pass** (agent ran script; printed event id). Harness sidecars sibling under `results/runs/`. Earlier run: `2026-04-08T15-34-12-720Z-scenario-03`.                                                                                                                                                   |
| 04  | [04-basics-go.md](scenarios/04-basics-go.md)                                   | `2026-04-08T15-48-31-367Z-scenario-04` | Pass (9/9)             | Pass      | Pass                       | `EVAL_LOCAL_DOCS=1`. Artifacts: `**main.go`**, `go.mod` (replace → repo `sdks/outpost-go`). `docs/agent-evaluation/.env` + `go run .`; tenant, destination, publish OK.                                                                                                                                                                                                                                                                                                                                                                                                           |
| 05  | [05-app-nextjs.md](scenarios/05-app-nextjs.md)                                 | `2026-04-08T16-12-10-708Z-scenario-05` | Pass (10/10)           | Pass      | Pass                       | **Last heuristic-pass run:** `**outpost-nextjs-demo/`** — simpler two-route app (`/api/register`, `/api/publish`), fixed topic. Richer app + assessment: **§ Scenario 05 — assessment** (`**nextjs-webhook-demo/`** in `2026-04-08T17-21-22-170Z-scenario-05`) — LLM + execution pass; heuristic **9/10** (`managed_base_not_selfhosted`, doc-corpus).                                                                                                                                                                                                                              |
| 06  | [06-app-fastapi.md](scenarios/06-app-fastapi.md)                               | `2026-04-09T08-38-42-008Z-scenario-06` | Pass (8/8)             | Pass      | Pass                       | `EVAL_LOCAL_DOCS=1`. `**main.py`** + `requirements.txt`, `outpost_sdk` + FastAPI. HTML: destinations list, add webhook (topics from API + URL), publish test event, delete. Execution: `python3 -m venv .venv`, `pip install -r requirements.txt`, run-dir `.env`, `uvicorn main:app` on :8766; **GET /** 200, **POST /destinations** 303, **POST /publish** 303.                                                                                                                                                                                                                 |
| 07  | [07-app-go-http.md](scenarios/07-app-go-http.md)                               | `2026-04-09T09-10-23-291Z-scenario-07` | Pass (9/9)             | Pass      | Pass                       | `EVAL_LOCAL_DOCS=1`. `**go-portal-demo/`** — `main.go` + `templates/`, `net/http`, `outpost-go` (`replace` → repo `sdks/outpost-go`). Multi-step create destination + **GET/POST /publish**. Execution: `PORT=8777` + key/base from `docs/agent-evaluation/.env`; **GET /** 200, **POST /publish** 200. Eval ~25 min wall time.                                                                                                                                                                                                                                                   |
| 08  | [08-integrate-nextjs-existing.md](scenarios/08-integrate-nextjs-existing.md)   | `2026-04-10T14-29-04-214Z-scenario-08` | Pass (10/10)           | Pass      | Pass                       | `EVAL_LOCAL_DOCS=1` + [scope-router prompt](../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc). Harness `**next-saas-starter/`** under run dir (gitignored). **Execution pass** — operator QA (Postgres, `.env`, migrate/seed/dev, Outpost UI/API). See **§ Scenario 08 — execution notes** for reproducibility (seed/`server-only`, destination-schema `key` vs SDK). Earlier: `2026-04-10T11-08-35-921Z-scenario-08` (8/8), `2026-04-09T14-48-16-906Z-scenario-08`, `2026-04-09T11-08-32-505Z-scenario-08`. |
| 09  | [09-integrate-fastapi-existing.md](scenarios/09-integrate-fastapi-existing.md) | `2026-04-10T19-54-20-037Z-scenario-09` | Pass (10/10)           | Pass      | Pass                       | `EVAL_LOCAL_DOCS=1`. **Artifact:** `full-stack-fastapi-template/` under run dir (**gitignored**). **Heuristic + LLM** from this stamp; harness sidecars sibling under `results/runs/`. Docker: default **5173** / **8000** / **1080** / **1025**; if host **5432** is taken, map DB e.g. **54334:5432** in `compose.override.yml`. After a **fresh DB volume**, clear the SPA token or **re-login** — stale JWT → **404 User not found** on `/api/v1/users/me` and `/api/v1/outpost/destinations`. **§ Scenario 09 — post-agent work** (below) still describes template fixes vs baseline. **Legacy runs:** `2026-04-10T19-22-02-903Z-scenario-09`, `2026-04-09T22-16-54-750Z-scenario-09` (6/6), `2026-04-09T20-48-16-530Z-scenario-09`, `2026-04-09T15-51-44-184Z-scenario-09`. |
| 10  | [10-integrate-go-existing.md](scenarios/10-integrate-go-existing.md)           | `2026-04-10T22-14-20-704Z-scenario-10` | Pass (7/7)             | Pass      | Pass                       | `EVAL_LOCAL_DOCS=1`. Harness clone **`startersaas-go-api/`** under run dir (**gitignored**); pin [**devinterface/startersaas-go-api**](https://github.com/devinterface/startersaas-go-api). **Execution:** `go build` OK; **`docker compose build`** fails on baseline **Go 1.21** image vs **`go 1.22`** in `go.mod` (upstream Dockerfile). **Smoke:** Mongo **:27018**, `go run .`, **`POST /api/v1/auth/signup`** with **`privacyAccepted` / `marketingAccepted` as JSON booleans** → **200**; log **`[outpost] published user.created`**. **Outpost delivery** to Hookdeck Source verified with a distinct **`POST /publish`** probe (tenant + webhook destination + event). |


### Scenario 08 — execution notes (`2026-04-10T14-29-04-214Z-scenario-08`)

**Execution:** **Pass** — operator QA on `**next-saas-starter/`** (artifact **not** committed; run folder under `results/runs/` is gitignored).

Reproducibility / gotchas:

- **`pnpm db:migrate`** — succeeds against local Postgres when `POSTGRES_URL` is set (see clone `README.md`).
- **`pnpm db:seed`** — as generated, importing `stripe` from `**lib/payments/stripe.ts**` pulls Outpost and `**server-only**`, which throws when the seed script runs under `**tsx**` (not the Next server). Common **local** fix: instantiate `**Stripe**` directly in `**lib/db/seed.ts**` with the same `**apiVersion**` as the payments module so seed does not load that file. Requires valid **Stripe** keys in `.env`. Re-running seed after a successful run fails on duplicate `**test@test.com**` — expected.
- **`pnpm dev`** — if another `**next dev**` already holds **`.next/dev/lock`** for this tree, stop it or remove the lock; port **3000** may be taken (Next picks another port). Turbopack may warn about multiple lockfiles when the app sits under the monorepo — see Next’s **`turbopack.root`** guidance if needed.
- **Destination schema `key`** — API returns `key` on schema fields; older SDK parses may strip it and break create-destination payloads keyed from labels. Regenerating SDKs (or a BFF raw fetch + mapping) aligns the UI with the API until then.

### Scenario 09 — post-agent work (representative: `2026-04-09T22-16-54-750Z-scenario-09`; latest eval stamp `2026-04-10T19-54-20-037Z-scenario-09`)

Work applied **after** the agent transcript so the FastAPI + React artifact matches current integration guidance (eval honesty + local execution). The template tree under `results/runs/<stamp>-scenario-09/` is **not committed** (see `results/.gitignore`); repo **docs** and **prompt** updates that back this scenario **are** in git.

**Frontend / router**

- **TanStack Router:** `frontend/src/routeTree.gen.ts` — register `/_layout/webhooks` (agent added the route file but not the generated tree).
- **API base URL:** webhooks page used browser-relative `/api/...` against nginx; switched to backend base (`OpenAPI.BASE` / `VITE_API_URL`).
- **Destination types:** Outpost JSON uses `**type`** and `**icon`** (not `id` / `svg`); fixed controlled radios / **Next** in the create wizard.

**Backend**

- `**POST /api/v1/webhooks/publish-test`** — synthetic `publish` for integration testing.
- `**GET /api/v1/webhooks/events`**, `**GET /api/v1/webhooks/attempts**`, `**POST /api/v1/webhooks/retry**` — BFF proxies for tenant-scoped **events list**, **attempts**, and **manual retry** (admin key server-side).

**Dashboard UI (webhooks page)**

- **Send test event**, **Event activity** (filter by destination, select event → attempts table, **Retry** on failed attempts).

**Docs & prompt (repository)**

- [Building your own UI](../content/guides/building-your-own-ui.mdoc) — destination-type field fixes; **Events, attempts, and retries** section (features, how they connect, links to API).
- [Agent prompt template](../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc) — full-stack guidance mentions **events list**, **attempts**, **retry**, alongside test publish.

### Scenario 09 — review notes (resolved, 2026-04-10)

Operator feedback from exercising the FastAPI full-stack artifact is **closed** in-repo:

1. **Event activity IA** — [Building your own UI](../content/guides/building-your-own-ui.mdoc) documents **default** destination → activity and **optional** tenant-wide activity with the same list endpoints; no open doc gap.
2. **Domain topics + real publishes vs test-only** — [Agent prompt](../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc) (topic reconciliation, domain publish, test publish as separate), scenarios **08–10** success criteria + user-turn scripts, [README](README.md) execution notes, and heuristic `**publish_beyond_test_only`** in `[src/score-transcript.ts](src/score-transcript.ts)` cover what we measure.

The **copied agent template** (the `## Hookdeck Outpost integration` block) intentionally stays **scenario-agnostic**: it does not name eval baselines, harness repos, or scenario IDs—only product-level integration guidance and doc links.

### Column hints


| Column            | Meaning                                                                                                    |
| ----------------- | ---------------------------------------------------------------------------------------------------------- |
| **Run directory** | Latest `results/runs/*-scenario-<NN>` with `heuristic-score.json` → `overallTranscriptPass: true` (folder contains `transcript.json`) |
| **Heuristic**     | `heuristic-score.json` → `overallTranscriptPass` (or `passed`/`total`)                                     |
| **LLM judge**     | `llm-score.json` → `overall_transcript_pass`                                                               |
| **Execution**     | Your smoke test of the **produced** script/app with real credentials — **not** automated by `npm run eval` |


### Status legend (suggested)

Use short text or symbols in cells, e.g. **Pass** / **Fail** / **Skip** / **N/A**, or ✅ / ❌ / —

---

## Scenario 05 — assessment (`2026-04-08T17-21-22-170Z`)

**Status:** Deep-dive on the **richer** Next.js artifact (`nextjs-webhook-demo/`). The **tracker table** row for scenario **05** points at **`2026-04-08T16-12-10-708Z-scenario-05`** (`outpost-nextjs-demo/`) as the **latest heuristic-pass** run (10/10); this section documents **`17-21-22`** separately because it failed that check while still passing LLM + execution.


| Dimension         | Result                                                                                                                                                                                                                                                                                                                            |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Run directory** | `results/runs/2026-04-08T17-21-22-170Z-scenario-05/`                                                                                                                                                                                                                                                                              |
| **Artifact**      | `nextjs-webhook-demo/` — Next.js App Router, `@hookdeck/outpost-sdk`, Outpost calls **only** in `app/api/**/route.ts` (managed API via SDK default unless `OUTPOST_API_BASE_URL` is set).                                                                                                                                         |
| **Heuristic**     | **9/10**; `overallTranscriptPass` false — single failure: `managed_base_not_selfhosted` because the transcript corpus included a **Read** of older [Building your own UI](../content/guides/building-your-own-ui.mdoc) containing `localhost:3333/api/v1`. The **generated app does not** use that URL. See § Scenario 05 heuristic. |
| **LLM judge**     | **Pass** — matches scenario 05 success criteria (Next.js structure, server-side SDK, distinct destination + publish UI, tenant/topic handling, README env, managed default).                                                                                                                                                      |
| **Execution**     | **Pass** (re-checked): `npm run build` in `nextjs-webhook-demo/`; `npm run dev` with `docs/agent-evaluation/.env`; `POST /api/destinations` → **201**, `POST /api/publish` → **200**.                                                                                                                                             |


**What the app demonstrates (UX / model):**

1. **Tenant** — Editable tenant id; copy states destinations and publishes are scoped to it.
2. **Register webhook destination** — URL field + **topic checkboxes** populated from `**GET /api/topics`** (server lists topics from Outpost); `**POST /api/destinations`** upserts tenant and creates webhook destination for selected topics.
3. **Destinations list** — `**GET /api/destinations?tenantId=`** table (type, target, topics) with refresh — matches “tenant → many destinations” mental model.
4. **Publish test event** — Separate action; `**POST /api/publish`** with chosen topic; UI notes fan-out to matching destinations.

**Comparison — older run `2026-04-08T16-12-10-708Z` (`outpost-nextjs-demo/`):** Simpler two-route app (`/api/register`, `/api/publish`), **fixed topic** in routes, **no** topics or destinations list APIs, **10/10** heuristic (no offending doc fragment in corpus). Useful as a minimal baseline; **17-21-22** is the richer assessment target.

---

## Scenario 05 heuristic — `managed_base_not_selfhosted`

Scenario 05 includes a regex check (`managed_base_not_selfhosted`) in `[src/score-transcript.ts](../src/score-transcript.ts)` (`scoreScenario05`). It looks at the **whole scoring corpus**: assistant-visible text **plus** content that ended up in the transcript from tools (e.g. **Read** of a doc file), not just files in the run folder.

- It fails if the corpus contains a **self-hosted** default API path: specifically the literal substring `localhost:3333/api/v1` (Outpost’s common local dev URL), or a similar `localhost:<port> / api/v1` pattern, unless `OUTPOST_API_BASE_URL` also appears (see code for the exact conditions).
- **Historical cause:** Older [Building your own UI](../content/guides/building-your-own-ui.mdoc) curl examples used `localhost:3333/api/v1`. If the agent **read** that page during a run, those lines were embedded in `transcript.json`, the check fired, and `overallTranscriptPass` became **false** even when the **generated Next.js app** only used the **managed** SDK default. That was a **harness / doc-corpus** interaction, not proof the app targeted local Outpost.
- **Doc update:** `docs/content/guides/building-your-own-ui.mdoc` was rewritten to be **managed / self-hosted agnostic** (`OUTPOST_API_BASE_URL`, OpenAPI-shaped paths). Examples **no longer contain** the literal `localhost:3333/api/v1`, so a future eval whose corpus only picks up the current file should **not** fail this check for that substring. Re-run scenario 05 to confirm; other `localhost` patterns could still match if they appear elsewhere in the corpus.
- **Run `2026-04-08T16-12-10-708Z`:** heuristic **10/10**, `overallTranscriptPass: true`.
- **Run `2026-04-08T17-21-22-170Z`:** heuristic **9/10**, `overallTranscriptPass: false` — failed `managed_base_not_selfhosted`; LLM judge still **passed**; transcript included **Read** of the **previous** `building-your-own-ui.mdx` with `localhost:3333/api/v1`.

**Possible follow-ups:** narrow the heuristic to tool-written files under the run workspace only, or exclude known doc paths from the substring that triggers this check.

## Action items

- Scenario 05: optionally re-run eval after the UI guide rewrite to confirm `managed_base_not_selfhosted` no longer false-positives on that doc **Read**; then consider whether the heuristic can be narrowed (see § above).

---

Full harness docs: [README.md](README.md).