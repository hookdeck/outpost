# Agent evaluation — results

This directory holds **manual run write-ups** and, under `**runs/`**, **automated** artifacts from `npm run eval`. Almost everything here is **gitignored** by default (see `[.gitignore](.gitignore)`).

Full workflow and env vars: `**[../README.md](../README.md)`**.

---

## Automated runs (`runs/`)

From `docs/agent-evaluation/`:

```sh
npm run eval -- --scenario 01
npm run eval -- --scenarios 01,02
npm run eval -- --all
```

Each run is a **directory** (same timestamp stem, all gitignored):

`runs/<stamp>-scenario-NN/`

| Path in run dir                         | What it is                                                                                                                       |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `transcript.json`                       | Full Claude Agent SDK transcript (`meta` + `messages`).                                                                          |
| `heuristic-score.json`                  | **Heuristic** transcript checks (`[../src/score-transcript.ts](../src/score-transcript.ts)`); rubrics **01–10** (`scoreScenario01`–`10`). |
| `llm-score.json`                        | **LLM judge** output (`[../src/llm-judge.ts](../src/llm-judge.ts)`) vs `**## Success criteria`** in the scenario markdown.       |
| *(other files)*                         | Anything the agent **`Write`**s (e.g. `outpost-quickstart.sh`); SDK **`cwd`** is this directory.                                 |

Legacy flat `runs/<stamp>-scenario-NN.json` (and `*.score.json` / `*.llm-score.json` beside it) still work with **`npm run score`**.

Re-score an existing run without re-running the agent:

```sh
npm run score -- --run results/runs/<stamp>-scenario-NN --write
npm run score -- --run results/runs/<stamp>-scenario-NN --llm --write
```

**Execution** (curl/SDK against live Outpost with `OUTPOST_API_KEY`) is **not** produced by these JSON files. Treat the **Execution (full pass)** rows in `[../scenarios/](../scenarios/)` as a separate human or CI step unless you add a verifier script.

---

## Manual run recordings

For **IDE-only** or ad-hoc runs (no `npm run eval`):

1. Copy `[RUN-RECORDING.template.md](RUN-RECORDING.template.md)` to a **local-only** name (e.g. `2026-04-08-s01-cursor.md`) in this directory.
2. Fill in transcript summary, heuristic/LLM pointers if you ran `npm run score` separately, **Execution verification**, and notes.
3. Do not commit raw recordings unless your policy allows it; anonymized summaries in a PR are fine.

Success criteria for every scenario: `**[../scenarios/*.md](../scenarios/)`** — section **Success criteria**.

---

## Template

See `[RUN-RECORDING.template.md](RUN-RECORDING.template.md)`.