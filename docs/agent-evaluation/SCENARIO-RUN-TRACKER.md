# Scenario run tracker

Use this table while you **run scenarios one at a time** and **execute the generated artifacts** against a real Outpost project.

## How to use

1. **Automated agent eval** (from `docs/agent-evaluation/`):
  ```sh
   npm run eval -- --scenario <NN>
  ```
   Each run creates `**results/runs/<ISO-stamp>-scenario-<NN>/**` with `transcript.json`, `heuristic-score.json`, `llm-score.json`, and whatever the agent wrote (scripts, apps, clones).
2. **Fill the table:** paste or note the **run directory** (stamp), mark **Heuristic** / **LLM** pass or fail (from the sidecars or console).
3. **Execution (generated code):** with `**OUTPOST_API_KEY`** (and `**OUTPOST_TEST_WEBHOOK_URL`** / `**OUTPOST_API_BASE_URL`** if needed) in your shell or `.env`, run the artifact the scenario expects — e.g. `bash outpost-quickstart.sh`, `npx tsx …`, `python …`, `go run …`, `npm run dev` in the generated app folder. Mark **Pass** / **Fail** / **Skip** and add **Notes** (HTTP status, delivery in Hookdeck Console, etc.). **Do not edit generated files to force a pass** — test what the agent produced; note OS/environment (e.g. Linux vs macOS) when relevant. **This column is the primary bar for “does the output actually work?”** Heuristic and LLM scores are supplementary.
4. **Optional:** copy a row to your local run log under `results/` if you use `RUN-RECORDING.template.md`.

---

## Tracker


| ID  | Scenario file                                                                  | Run directory (`results/runs/…`) | Heuristic | LLM judge | Execution (generated code) | Notes |
| --- | ------------------------------------------------------------------------------ | -------------------------------- | --------- | --------- | -------------------------- | ----- |
| 01  | [01-basics-curl.md](scenarios/01-basics-curl.md)                               | `2026-04-08T14-58-40-850Z-scenario-01` | Pass (7/7) | Pass | — | Eval exit 0. Artifact: **`try-it-out.sh`**. **Execution** (manual): set `OUTPOST_API_KEY`, run script; uses `curl --fail-with-body` (2xx includes **202** on publish). |
| 02  | [02-basics-typescript.md](scenarios/02-basics-typescript.md)                   |                                  |           |           |                            |       |
| 03  | [03-basics-python.md](scenarios/03-basics-python.md)                           |                                  |           |           |                            |       |
| 04  | [04-basics-go.md](scenarios/04-basics-go.md)                                   |                                  |           |           |                            |       |
| 05  | [05-app-nextjs.md](scenarios/05-app-nextjs.md)                                 |                                  |           |           |                            |       |
| 06  | [06-app-fastapi.md](scenarios/06-app-fastapi.md)                               |                                  |           |           |                            |       |
| 07  | [07-app-go-http.md](scenarios/07-app-go-http.md)                               |                                  |           |           |                            |       |
| 08  | [08-integrate-nextjs-existing.md](scenarios/08-integrate-nextjs-existing.md)   |                                  |           |           |                            |       |
| 09  | [09-integrate-fastapi-existing.md](scenarios/09-integrate-fastapi-existing.md) |                                  |           |           |                            |       |
| 10  | [10-integrate-go-existing.md](scenarios/10-integrate-go-existing.md)           |                                  |           |           |                            |       |


### Column hints


| Column            | Meaning                                                                                                    |
| ----------------- | ---------------------------------------------------------------------------------------------------------- |
| **Run directory** | e.g. `2026-04-07T15-00-00-000Z-scenario-01` — the folder containing `transcript.json`                      |
| **Heuristic**     | `heuristic-score.json` → `overallTranscriptPass` (or `passed`/`total`)                                     |
| **LLM judge**     | `llm-score.json` → `overall_transcript_pass`                                                               |
| **Execution**     | Your smoke test of the **produced** script/app with real credentials — **not** automated by `npm run eval` |


### Status legend (suggested)

Use short text or symbols in cells, e.g. **Pass** / **Fail** / **Skip** / **N/A**, or ✅ / ❌ / —

---

## Action items

Add bullet or table rows here when something should be tracked across runs (docs gaps, harness changes, etc.). *None recorded yet for this pass.*

---

Full harness docs: [README.md](README.md).