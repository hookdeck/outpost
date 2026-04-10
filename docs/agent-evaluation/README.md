# Agent evaluation — Hookdeck Outpost onboarding

This folder contains **manual** scenario specs (markdown) and an **automated** runner that uses the [Claude Agent SDK](https://platform.claude.com/docs/en/agent-sdk/overview) (`src/run-agent-eval.ts`).

**Authoring standards (user-turn wording, no eval leakage):** [`AGENTS.md`](AGENTS.md) — also enforced via [`.cursor/rules/agent-evaluation-authoring.mdc`](../../.cursor/rules/agent-evaluation-authoring.mdc) when editing here.

## Where success criteria live

| What | Where |
|------|--------|
| **Human checklist** (full eval, including execution) | Each file under [`scenarios/`](scenarios/) — section **Success criteria** (static + **Execution (full pass)** rows). |
| **Manual run write-up** | [`results/RUN-RECORDING.template.md`](results/RUN-RECORDING.template.md) — copy to a local file under `results/` (gitignored). |
| **Automated transcript rubric** (regex heuristics) | [`src/score-transcript.ts`](src/score-transcript.ts) — `scoreScenario01`–`scoreScenario10` (assistant text + tool-written file corpus). Scenarios **08–10** include **`publish_beyond_test_only`** (domain publish signal vs test-only). |
| **LLM judge** (Anthropic vs **`## Success criteria`** in each scenario) | [`src/llm-judge.ts`](src/llm-judge.ts) — runs after each scenario unless **`--no-score-llm`**; also `npm run score -- --llm`. |

**Deliberate scope:** `npm run eval` **requires** **`--scenario`**, **`--scenarios`**, or **`--all`**. There is no silent “run everything” default — you choose the scenarios and accept the cost. After **each** run: **`transcript.json`**, **`heuristic-score.json`**, and **`llm-score.json`** (judge reads the same **Success criteria** as humans). Exit **1** if any enabled score fails.

Opt out of scoring: **`--no-score`** (heuristic only), **`--no-score-llm`** (drops the Success-criteria judge), or **`.env`**: **`EVAL_NO_SCORE_HEURISTIC=1`**, **`EVAL_NO_SCORE_LLM=1`**. Transcript-only: **`npm run eval -- --no-score --no-score-llm`**.

Each scenario run uses one directory:

`results/runs/<ISO-stamp>-scenario-NN/`

- **`transcript.json`** — full SDK log (written only **after** the agent finishes all turns — long runs may show little console output until then)
- **Harness sidecars (siblings of the run folder, not inside it)** — so the agent sandbox cannot read them:
  - **`<stamp>-scenario-NN.eval-started.json`** — written when the scenario begins (pid, scenario id, paths)
  - **`<stamp>-scenario-NN.eval-failure.json`** — uncaught exception before `transcript.json`
  - **`<stamp>-scenario-NN.eval-aborted.json`** — **SIGTERM** / **SIGINT** before completion (not **SIGKILL**)
  If **`transcript.json`** is missing, check these files next to **`…/runs/<stamp>-scenario-NN/`** (same directory as the run folder, not inside it).
- **`heuristic-score.json`** / **`llm-score.json`** — by default (unless disabled above)
- **Agent-written files** — the SDK **`cwd`** is this directory. Defaults include **`Write`**, **`Edit`**, and **`Bash`** for clones, installs, and generated code.

Re-score a finished run without re-invoking the agent — uses **today’s** [`src/score-transcript.ts`](src/score-transcript.ts) and **scenario markdown on disk** (so LLM criteria update when you edit **`## Success criteria`**):

- **`npm run score -- --run results/runs/<dir> --write`** — refresh **`heuristic-score.json`**
- Add **`--llm`** to also re-run the judge and write **`llm-score.json`** (needs **`ANTHROPIC_API_KEY`**)

Legacy flat files `*-scenario-NN.json` next to `runs/` are still accepted by **`npm run score`** for older runs.

**Execution** (live Outpost) is still not auto-verified; the LLM is instructed to set `execution_in_transcript.pass` to **null** unless the transcript itself reports HTTP results.

## Automated runs (Claude Agent SDK)

From `docs/agent-evaluation/`:

```sh
npm install
cp .env.example .env   # then edit: ANTHROPIC_API_KEY, EVAL_TEST_DESTINATION_URL, …
npm run eval -- --scenario 01
npm run eval -- --scenarios 01,02,08
npm run eval -- --all   # explicit full suite (every scenario file)
npm run eval:ci         # same as --scenarios 01,02 + heuristic + LLM judge (see § CI)
npm run eval -- --dry-run
```

The runner loads **`docs/agent-evaluation/.env`** automatically (via `dotenv`). Shell exports still override `.env` if both are set.

### Wall time (scenarios **08–10** and other heavy baselines)

Scenarios that **`git clone`** a full SaaS template and run **`npm` / `pnpm` / `docker compose`** installs are **slow by design**. Expect **roughly 30–90+ minutes** of wall time for a single run of **08**, **09**, or **10** (clone + install + several agent turns). The harness prints little to the terminal until **`transcript.json`** is written at the end, which can look hung.

- **Progress on stderr:** set **`EVAL_PROGRESS=1`** so the runner prints **periodic lines** (default every **30s** per agent query, plus every **25** SDK messages). You still see activity when the agent is inside a **long Bash** call and the SDK emits **no** new messages for a while. Tune with **`EVAL_PROGRESS_INTERVAL_MS`** (minimum **5000**). Default is off so CI and short runs stay quiet.
- **Stop early:** **Ctrl+C** (**SIGINT**) in the terminal running `npm run eval`. The runner writes **`*-scenario-NN.eval-aborted.json`** next to the run folder (see **Harness sidecars** at the top of this file).
- **Skip re-clone:** If the baseline is already under the run directory, **`EVAL_SKIP_HARNESS_PRE_STEPS=1`** skips **`git_clone`** from the scenario harness (see each scenario’s **`## Eval harness`** block).
- **Cap agent length (smoke only):** **`EVAL_MAX_TURNS`** (default **80**) limits SDK turns; lowering it may end the run sooner but often **fails** the integration before success criteria are met—use for debugging, not a real pass.
- **Save judge time only:** **`--no-score-llm`** skips the Success-criteria LLM judge at the end (saves a few minutes; you lose that rubric).

For **fast** automated signal in CI, use **`eval:ci`** (**01** + **02** only)—not **08**.

### CI (recommended slice)

For **pull-request or main-branch** automation, run **two** scenarios only:

| Scenario | Why |
|----------|-----|
| **01** (curl) | Shortest path: managed API, tenant → destination → publish, no `npm install` / framework scaffold. Cheap signal that the prompt + heuristics still align with the curl quickstart. |
| **02** (TypeScript) | Most common integration style: **`@hookdeck/outpost-sdk`**, env vars, same API flow in code. Still much faster than **05** (Next.js) or **08** (clone a full SaaS repo). |

**Commands:**

```sh
cd docs/agent-evaluation && npm ci && npm run eval:ci
# or: ./scripts/ci-eval.sh   # requires ANTHROPIC_API_KEY + EVAL_TEST_DESTINATION_URL in the environment
# after a successful eval:ci, live Outpost smoke: OUTPOST_API_KEY + OUTPOST_TEST_WEBHOOK_URL ./scripts/execute-ci-artifacts.sh
```

`eval:ci` is **`npm run eval -- --scenarios 01,02`**: both **heuristic** checks and the **LLM judge** (grounded in each scenario’s **`## Success criteria`**). Skipping the judge would leave you with regex-only signal, which does not encode the product checklist.

**GitHub Actions:** add repository secrets **`ANTHROPIC_API_KEY`**, **`EVAL_TEST_DESTINATION_URL`**, and **`OUTPOST_API_KEY`**. Workflow **`.github/workflows/docs-agent-eval-ci.yml`** runs **`./scripts/ci-eval.sh`** with **`EVAL_LOCAL_DOCS=1`** (agent **reads docs from the repo**), then **`./scripts/execute-ci-artifacts.sh`**: picks the **newest** **`*-scenario-01`** / **`*-scenario-02`** pair from **`results/runs/`**, runs the generated **`.sh`** then **`npx tsx`** on the TypeScript artifact (**`npm install`** in the **02** run dir when **`package.json`** exists). **`OUTPOST_TEST_WEBHOOK_URL`** in CI is set from the same secret as **`EVAL_TEST_DESTINATION_URL`**. Triggers on **`workflow_dispatch`** (manual: Actions → **Docs agent eval (CI slice)** → **Run workflow**, pick branch), pushes to **`main`**, and **pull requests** when **`docs/content/**`**, **`docs/apis/**`**, **`sdks/outpost-typescript/**`**, root **`docs/README.md`** / **`docs/AGENTS.md`**, or **`docs/agent-evaluation/**`** change (GitHub does not allow **`paths`** + **`paths-ignore`** together on the same event, so edits under e.g. **`docs/agent-evaluation/README.md`** also match **`docs/agent-evaluation/**`** and can trigger a run). Uses **`ubuntu-latest`** (Claude Agent SDK needs normal filesystem access — avoid tight sandboxes; see **Permissions / failures** above). **Fork PRs** skip this job (secrets are not available).

- **`ANTHROPIC_API_KEY`** — required for the agent and for the **LLM judge** (Success criteria) after each scenario you run.
- **`EVAL_TEST_DESTINATION_URL`** — required for Turn 0; same Source URL as `{{TEST_DESTINATION_URL}}` (and, in CI, reused as **`OUTPOST_TEST_WEBHOOK_URL`** for execution).
- **`OUTPOST_API_KEY`** — required for **`execute-ci-artifacts.sh`** and for **GitHub Actions** execution after **`eval:ci`**. For **local** transcript-only runs you can omit it. Put the key in **`docs/agent-evaluation/.env`** (or export); never paste it into chat.
- **`EVAL_LOCAL_DOCS=1`** — Turn 0 replaces public doc URLs with **absolute paths to MDX/OpenAPI files in this repo** (agent uses **Read** on **`docs/`** instead of **WebFetch** to production). Use locally when validating unpublished docs; **GitHub Actions** sets this for **`docs-agent-eval-ci.yml`**.
- **`EVAL_SKIP_HARNESS_PRE_STEPS=1`** — skip **`git_clone`** (and any future **`preSteps`**) declared in a scenario’s **`## Eval harness`** JSON block; useful offline or when the baseline folder is already present.

- **Turn 0** text is built from [`hookdeck-outpost-agent-prompt.mdoc`](../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc) (`## Template`) with placeholders filled from environment variables.
- Transcripts are written to `results/runs/<stamp>-scenario-NN/transcript.json` (gitignored).

See `npm run eval -- --help` for env vars (`EVAL_TOOLS`, `EVAL_MODEL`, etc.).

### Permissions / failures (why a run might not work)

Two different things get called “permissions”:

1. **Cursor (or CI) sandbox and `tsx`** — The `tsx` **CLI** opens an IPC pipe in `/tmp` (or similar), which some sandboxes block (`listen EPERM`). This repo’s `npm run eval` uses **`node --import tsx`** instead so Node loads the tsx **loader** only (no CLI IPC). If you still see EPERM, run the same command in a normal terminal outside the sandbox, or use `npm run eval:tsx-cli` only where IPC is allowed.

2. **Claude Agent SDK `dontAsk` + `allowedTools`** — In `dontAsk` mode, tools **not** listed in `allowedTools` are denied (no prompt). Defaults include **`Write`**, **`Edit`**, and **`Bash`** so app scenarios can scaffold and install dependencies inside the per-run directory. With **`EVAL_LOCAL_DOCS=1`**: **`Read,Glob,Grep,Write,Edit,Bash`**. Otherwise **`Read,Glob,Grep,WebFetch,Write,Edit,Bash`**. Narrow **`EVAL_TOOLS`** only if you need a stricter harness (e.g. transcript-only, no shell).

3. **Run-directory sandbox (`PreToolUse`)** — Under `permissionMode: dontAsk`, hooks enforce boundaries (not `canUseTool` alone):
   - **Write / Edit / NotebookEdit** — target path must resolve under `results/runs/<stamp>-scenario-NN/`. **`EVAL_DISABLE_WORKSPACE_WRITE_GUARD=1`** disables this only (debug).
   - **Read / Glob / Grep** — must stay under that same run directory, and (when **`EVAL_LOCAL_DOCS=1`**) under **`docs/`** of the Outpost repo for local MDX/OpenAPI only. **`EVAL_DISABLE_WORKSPACE_READ_GUARD=1`** disables read/glob/grep/bash/agent checks (restores pre–workspace-sandbox behavior).
   - **Bash** — commands must not reference the Outpost **`repositoryRoot`** on disk unless the reference stays inside the run dir or (with local docs) inside **`docs/`**.
   - **Agent** (subagent) — **denied by default** so runs cannot spider the monorepo for “free” SDK context. **`EVAL_ALLOW_AGENT_TOOL=1`** to opt in.
   - Turn 0 also appends a short **workspace boundary** block (absolute run-dir paths) so the model treats only the clone as the product under integration.

Changing **`EVAL_PERMISSION_MODE`** is usually unnecessary; widening **`EVAL_TOOLS`** (or using local docs) fixes most tool denials.

### Transcript vs execution (full pass)

`npm run eval` only captures **what the model produced**; by itself it does **not** call Outpost (transcript review). **`./scripts/execute-ci-artifacts.sh`** (and the **GitHub Actions** workflow’s second step) runs the **01** shell + **02** TypeScript outputs against **live** Outpost when **`OUTPOST_API_KEY`** and **`OUTPOST_TEST_WEBHOOK_URL`** are set.

A **full pass** also answers: *did the generated curl / script / app succeed against a live Outpost project?* Each scenario’s **Success criteria** ends with **Execution** checkboxes for that step. To run them:

1. Add **`OUTPOST_API_KEY`** (and **`OUTPOST_TEST_WEBHOOK_URL`** / **`OUTPOST_API_BASE_URL`** when the artifact expects them) to `docs/agent-evaluation/.env` so your shell has them after `dotenv` or when you `source` / copy into the directory where you run the code.
2. Run the agent’s commands or start its app and complete the flows the scenario describes.
3. Record pass/fail in your run notes ([`results/RUN-RECORDING.template.md`](results/RUN-RECORDING.template.md)).

#### Integration scenarios (08–10): depth to verify

These measure **existing-app integration**, not a greenfield demo. When you **execute** the artifact:

- **Topic reconciliation:** Confirm README maps **`publish` topics** to **real domain events** and, when the **configured topic list from onboarding** is incomplete, tells the operator to **add topics in Hookdeck**—not to retarget the app to a stale list (unless the scenario was explicitly wiring-only).
- **Domain publish:** Prefer a smoke step that performs a **real product action** (signup, create entity, etc.) and observe an accepted publish—not **only** a “send test event” button.
- **Heuristic `publish_beyond_test_only`:** [`score-transcript.ts`](src/score-transcript.ts) adds a weak automated check that the transcript corpus suggests publish beyond synthetic test-only paths; it is **not** a substitute for execution or the LLM judge reading **Success criteria**.

## Single source of truth for the dashboard prompt

The **full prompt template** (the text operators paste as Turn 0) lives in **one** place:

**[`docs/content/quickstarts/hookdeck-outpost-agent-prompt.mdoc`](../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc)** — use the fenced block under **## Template**.

For eval runs, example placeholder substitutions (non-secret) are in [`fixtures/placeholder-values-for-turn0.md`](fixtures/placeholder-values-for-turn0.md) only. That file intentionally **does not** duplicate the template.

The Hookdeck dashboard should eventually render the **same** template body from product-side source; until then, this MDX page is the documentation canonical copy.

## How to run an evaluation (manual)

1. **Turn 0:** Open the [agent prompt template](../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc), copy **## Template**, replace `{{…}}` (see [placeholder examples](fixtures/placeholder-values-for-turn0.md)).
2. **Pick a scenario:** e.g. [`scenarios/01-basics-curl.md`](scenarios/01-basics-curl.md).
3. **New agent thread:** Paste Turn 0, then follow each **Turn N — User** line from the scenario verbatim (or as specified).
4. **Judge output:** Use the scenario’s **Success criteria** checkboxes (human decision).
5. **Record:** Copy [`results/RUN-RECORDING.template.md`](results/RUN-RECORDING.template.md) to a local filename under `results/` (see [`results/README.md`](results/README.md)); those files are **gitignored** by default.

### Helper script (optional)

From the repo root:

```sh
./docs/agent-evaluation/scripts/run-scenario.sh 01
```

This **only prints** paths and reminders. It does **not** start an agent or call OpenAI/Anthropic/etc.

## Judging results

- **Automated runs:** use **Success criteria** in each `scenarios/*.md` (definition of pass). Each **`npm run eval -- --scenario|scenarios|all`** run applies **heuristic + LLM** scorers unless you pass **`--no-score`** / **`--no-score-llm`**; **Execution** rows stay manual unless you add a verifier.
- **Manual runs** use the checklist in [`results/RUN-RECORDING.template.md`](results/RUN-RECORDING.template.md).

There is still **no single portable “IDE agent” CLI** for all vendors; the SDK runner is the supported path for headless Anthropic-based CI.

## Measuring scenarios

| Layer | What it answers | Where |
|--------|-----------------|--------|
| **Definition** | What “good” means (product + transcript) | **`## Success criteria`** in each [`scenarios/*.md`](scenarios/) |
| **Heuristic** | Fast, deterministic signal from transcript JSON | [`src/score-transcript.ts`](src/score-transcript.ts) — combines assistant text with **Write/Edit tool inputs** and tool results so on-disk artifacts count |
| **LLM judge** | Structured pass/fail vs the same **Success criteria** | After each scenario when **`--no-score-llm`** is not set; or `npm run score -- --run <dir> --llm` — [`src/llm-judge.ts`](src/llm-judge.ts) |
| **Execution** | Live API / app smoke test | Human (or future script); not automated here |

**Heuristic functions** (failed checks set **`npm run eval`** / **`npm run score`** exit **1** when that scorer ran):

| Scenario | Function | Topics covered (summary) |
|----------|----------|---------------------------|
| 01 | `scoreScenario01` | Managed URL, tenant PUT, webhook destination POST, publish `data`, no key leak, optional verify turn |
| 02 | `scoreScenario02` | TS SDK, `Outpost`, env key, tenants/destinations/publish, webhook env, run command |
| 03 | `scoreScenario03` | Python SDK import, client, same API calls, env, webhook URL |
| 04 | `scoreScenario04` | Go module, `New`/`WithSecurity`, Upsert/Create/Publish, env, webhook URL |
| 05 | `scoreScenario05` | Next.js signals, TS SDK, API routes, two flows, server env key, no `NEXT_PUBLIC_` key, README, optional stress-turn Hookdeck hint |
| 06 | `scoreScenario06` | FastAPI, `outpost_sdk`, uvicorn, server env, two flows, README, webhook docs |
| 07 | `scoreScenario07` | `net/http`, Go SDK + `CreateDestinationCreateWebhook`, HTML UI, two flows, `go run`, README |
| 08 | `scoreScenario08` | Clone **next-saas-starter** (or git baseline), TS SDK, publish/destinations/tenants, server env key, per-customer webhook story |
| 09 | `scoreScenario09` | Clone **full-stack-fastapi-template** (or git baseline), `outpost_sdk`, integration + domain hook, env key, no client `NEXT_PUBLIC_`/`VITE_` key wiring, `publish_beyond_test_only`, README/env docs signal |
| 10 | `scoreScenario10` | Clone **startersaas-go-api** (or git baseline), Go Outpost SDK, publish + handler hook, env key |

Export **`SCENARIO_IDS_WITH_HEURISTIC_RUBRIC`** in `score-transcript.ts` lists IDs **01–10** for tooling.

## Scenarios

To record each **`npm run eval -- --scenario …`** run, automated scores, and **whether you ran the generated code** with `OUTPOST_API_KEY`, use **[`SCENARIO-RUN-TRACKER.md`](SCENARIO-RUN-TRACKER.md)** (committed; not under `results/`, which is gitignored).

| ID | File | Goal |
|----|------|------|
| 1 | [scenarios/01-basics-curl.md](scenarios/01-basics-curl.md) | Minimal **curl** only (managed API). |
| 2 | [scenarios/02-basics-typescript.md](scenarios/02-basics-typescript.md) | Minimal **TypeScript** script (`@hookdeck/outpost-sdk`). |
| 3 | [scenarios/03-basics-python.md](scenarios/03-basics-python.md) | Minimal **Python** script (`outpost_sdk`). |
| 4 | [scenarios/04-basics-go.md](scenarios/04-basics-go.md) | Minimal **Go** program (`outpost-go`). |
| 5 | [scenarios/05-app-nextjs.md](scenarios/05-app-nextjs.md) | Small **Next.js** app: UI to register a webhook destination and trigger a test publish. |
| 6 | [scenarios/06-app-fastapi.md](scenarios/06-app-fastapi.md) | Small **FastAPI** app with the same UX as scenario 5. |
| 7 | [scenarios/07-app-go-http.md](scenarios/07-app-go-http.md) | Small **Go** `net/http` app + simple HTML UI (same UX as scenario 5). |
| 8 | [scenarios/08-integrate-nextjs-existing.md](scenarios/08-integrate-nextjs-existing.md) | **Existing Next.js SaaS** baseline — add outbound webhooks via Outpost ([leerob/next-saas-starter](https://github.com/leerob/next-saas-starter)). |
| 9 | [scenarios/09-integrate-fastapi-existing.md](scenarios/09-integrate-fastapi-existing.md) | **Existing FastAPI full-stack** baseline — Outpost integration ([fastapi/full-stack-fastapi-template](https://github.com/fastapi/full-stack-fastapi-template)). |
| 10 | [scenarios/10-integrate-go-existing.md](scenarios/10-integrate-go-existing.md) | **Existing Go SaaS API** baseline — Outpost integration ([devinterface/startersaas-go-api](https://github.com/devinterface/startersaas-go-api)). |

Scenarios **1–4** align with **“Try it out”**; **5–7** with **“Build a minimal example”**; **8–10** with **“Integrate with an existing app”** using pinned OSS baselines (Java / .NET can be added later the same way).

## Agent skills recommendation

**Recommend yes** for teams standardizing on Hookdeck’s skill pack: the [outpost skill](https://github.com/hookdeck/agent-skills/tree/main/skills/outpost) gives agents a consistent overview (tenants, destinations, topics, curl shape) and links into docs.

**Caveats (update the skill in `hookdeck/agent-skills`, not in this repo):**

1. **Managed-first** — The published skill is still **self-hosted heavy** (Docker block first; managed is a short table). For Hookdeck Outpost GA, the skill should foreground [managed quickstarts](../content/quickstarts/hookdeck-outpost-curl.mdoc), `https://api.outpost.hookdeck.com/2025-07-01`, **Settings → Secrets**, and `OUTPOST_API_KEY` / optional `OUTPOST_API_BASE_URL` to match product copy.
2. **REST paths** — Examples must use **`/tenants/{id}`**, not `PUT $BASE_URL/$TENANT_ID` (that path is wrong for the real API).
3. **Naming** — Align env var naming with docs (`OUTPOST_API_KEY` or documented dashboard name), not ad-hoc `HOOKDECK_API_KEY` unless the dashboard literally uses that string.
4. **Router vs. deep skills** — Today `outpost` is one monolithic `SKILL.md`. The skill itself mentions **future** destination-specific skills (`outpost-webhooks`, etc.). For scale, consider either **sections** with clear headings or **child skills** (e.g. `outpost-managed-quickstart`, `outpost-self-hosted`) once content grows—without forcing users to install many tiles for the common case.

Until the skill is updated, agents should still be pointed at the **quickstart MDX pages** in this repo (or production docs URLs); the skill is supplementary.

## Related docs

- [Agent prompt template (SSoT)](../content/quickstarts/hookdeck-outpost-agent-prompt.mdoc)
- [Upstream skill notes](SKILL-UPSTREAM-NOTES.md)
- [TEMP tracking note](../TEMP-hookdeck-outpost-onboarding-status.md)
