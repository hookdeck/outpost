# Hookdeck Outpost onboarding — status (temporary)

**Purpose:** Track implementation status for the managed quickstarts, agent prompt, and related work. **Delete this file** when tracking moves elsewhere (e.g. Linear, parent epic).

**Last updated:** 2026-04-07

---

## Agent eval harness — **implemented**; **prompt validation in progress**

The automated harness in `docs/agent-evaluation/` is in place. **What it does today:**

| Area | Status |
|------|--------|
| **Runner** | `src/run-agent-eval.ts` — **## Template** from `hookdeck-outpost-agent-prompt.mdx`, `{{…}}` from env, multi-turn scenarios, **Claude Agent SDK** with **`Read` / `Glob` / `Grep` / `WebFetch` / `Write` / `Edit` / `Bash`**, **`cwd`** = `results/runs/<stamp>-scenario-NN/` |
| **Artifacts** | `transcript.json`, optional **`heuristic-score.json`** + **`llm-score.json`** (LLM reads each scenario **`## Success criteria`**), agent-written files beside the transcript |
| **Heuristics** | `score-transcript.ts` — **`scoreScenario01`–`scoreScenario10`** on assistant text + tool corpus (so **Write**/Edit content counts) |
| **Scenarios** | **01–04:** try-it-out (curl, TS, Python, Go). **05–07:** minimal UIs (Next, FastAPI, Go `net/http`). **08–10:** Option 3 — integrate into pinned repos (Next **`leerob/next-saas-starter`**, FastAPI **`philipokiokio/FastAPI_SAAS_Template`**, Go **`devinterface/startersaas-go-api`**) |
| **CLI** | **`npm run eval` requires `--scenario`, `--scenarios`, or `--all`** — no accidental full-suite run. Default scoring = **heuristic + LLM judge** unless **`--no-score`** / **`--no-score-llm`** or **`EVAL_NO_SCORE_*`**. **Exit 1** if any enabled score fails |
| **CI** | **`npm run eval:ci`** = **`--scenarios 01,02`** + heuristic **and** LLM judge. **`scripts/ci-eval.sh`** — requires **`ANTHROPIC_API_KEY`**, **`EVAL_TEST_DESTINATION_URL`** |
| **Re-score** | `npm run score -- --run <run-dir> [--llm] [--write]` |

**Operational**

- Prefer a normal runner / full permissions for session persistence (`~/.claude/...`); tight sandboxes can break multi-turn resume.
- **Validate the prompt in stages** (simple → complex); exact commands below.

### Recommended run order (test evals → stress prompt)

Run from **`docs/agent-evaluation/`** with **`.env`** set (**`ANTHROPIC_API_KEY`**, **`EVAL_TEST_DESTINATION_URL`**). Use a normal terminal (not a restricted sandbox) for reliable SDK sessions.

**Stage A — basics (fast, minimal tooling)**

```sh
npm run eval -- --scenarios 01,02,03,04
```

**Stage B — minimal example apps**

```sh
npm run eval -- --scenarios 05,06,07
```

**Stage C — existing-app integration (clone + integrate; slowest)**

```sh
npm run eval -- --scenarios 08,09,10
```

**Full suite (explicit cost)**

```sh
npm run eval -- --all
```

After each stage, inspect **`results/runs/<stamp>-scenario-NN/`** (transcript, scores, on-disk artifacts). **Goal:** confirm the **dashboard prompt** + **Success criteria** hold across stacks; **Execution** (live **`OUTPOST_API_KEY`**) remains a separate human step per scenario.

---

## Agent eval automation (original plan — historical)

1. **In-repo runner** — ✅ Node + Agent SDK (not shell-only `curl`).
2. **Default backend: Anthropic** — ✅ Agent SDK.
3. **Claude Code CLI** — Optional local path only (unchanged).
4. **OpenAI adapter** — Still optional / not implemented.
5. **Judging** — ✅ Transcripts on disk; ✅ heuristics; ✅ LLM-as-judge vs **`## Success criteria`**.
6. **CI shape** — ✅ `eval:ci` + docs; **GitHub Actions workflow** not committed (add `workflow_dispatch` + secrets when ready).

**Avoid as primary design:** brittle hand-rolled JSON in bash, or CLI-only gates that break for contributors and headless runners.

---

## Done (Outpost OSS repo)

- Managed quickstarts: `hookdeck-outpost-curl.mdx`, `-typescript.mdx`, `-python.mdx`, `-go.mdx`
- Agent prompt template page: `hookdeck-outpost-agent-prompt.mdx` (includes **Files on disk** guidance)
- Zudoku sidebar: **Quickstarts → Hookdeck Outpost** (above **Self-Hosted**)
- `quickstarts.mdx` index: managed vs self-hosted links
- Content aligned with product copy: API key from **Settings → Secrets**, verify via Hookdeck Console + project logs
- SDK quickstarts: env vars, step-commented scripts
- **Agent evaluation:** `docs/agent-evaluation/` — scenarios **01–10**, dual scoring, explicit CLI, CI slice, **`SCENARIO-RUN-TRACKER.md`** (per-scenario + execution log), `results/README.md`, `fixtures/`, `SKILL-UPSTREAM-NOTES.md`

## Pending / follow-up

- **Prompt + eval validation (in progress):** Run stages **A → B → C** above (or **`--all`** when deliberate); record pass/fail per scenario; adjust prompt or heuristics if systematic failures appear
- **hookdeck/agent-skills:** Refresh `skills/outpost/SKILL.md` using `docs/agent-evaluation/SKILL-UPSTREAM-NOTES.md` (managed-first, correct `/tenants/` paths, env naming)
- **QA:** Run TypeScript, Python, and Go examples against live managed API; confirm production doc links
- **Test destination URL:** When Console has a stable public URL story, align quickstarts if copy changes
- **Hookdeck Dashboard:** Two-step onboarding (topics → copy agent prompt) with placeholder injection; env UI for `OUTPOST_API_KEY` (not in prompt body)
- **Hookdeck Astro site:** MDX, `llms.txt` / `llms-full.txt`, canonical `DOCS_URL`
- **CI workflow:** Optional GitHub Actions job for `eval:ci` with secrets
- **Deferred (not blocking GA):** Broader docs IA per original plan

## References

- OpenAPI / managed base URL: `https://api.outpost.hookdeck.com/2025-07-01` (in `docs/apis/openapi.yaml` `servers`)
- Agent template source: `docs/pages/quickstarts/hookdeck-outpost-agent-prompt.mdx`
- Eval harness: `docs/agent-evaluation/README.md`
