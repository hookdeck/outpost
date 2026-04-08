# Agent evaluation — Hookdeck Outpost onboarding

This folder contains **manual** scenario specs (markdown) and an **automated** runner that uses the [Claude Agent SDK](https://platform.claude.com/docs/en/agent-sdk/overview) (`src/run-agent-eval.ts`).

## Where success criteria live

| What | Where |
|------|--------|
| **Human checklist** (full eval, including execution) | Each file under [`scenarios/`](scenarios/) — section **Success criteria** (static + **Execution (full pass)** rows). |
| **Manual run write-up** | [`results/RUN-RECORDING.template.md`](results/RUN-RECORDING.template.md) — copy to a local file under `results/` (gitignored). |
| **Automated transcript rubric** (regex heuristics) | [`src/score-transcript.ts`](src/score-transcript.ts) — `scoreScenario01`–`scoreScenario10` (assistant text + tool-written file corpus). |
| **LLM judge** (Anthropic vs **`## Success criteria`** in each scenario) | [`src/llm-judge.ts`](src/llm-judge.ts) — runs after each scenario unless **`--no-score-llm`**; also `npm run score -- --llm`. |

**Deliberate scope:** `npm run eval` **requires** **`--scenario`**, **`--scenarios`**, or **`--all`**. There is no silent “run everything” default — you choose the scenarios and accept the cost. After **each** run: **`transcript.json`**, **`heuristic-score.json`**, and **`llm-score.json`** (judge reads the same **Success criteria** as humans). Exit **1** if any enabled score fails.

Opt out of scoring: **`--no-score`** (heuristic only), **`--no-score-llm`** (drops the Success-criteria judge), or **`.env`**: **`EVAL_NO_SCORE_HEURISTIC=1`**, **`EVAL_NO_SCORE_LLM=1`**. Transcript-only: **`npm run eval -- --no-score --no-score-llm`**.

Each scenario run uses one directory:

`results/runs/<ISO-stamp>-scenario-NN/`

- **`transcript.json`** — full SDK log  
- **`heuristic-score.json`** / **`llm-score.json`** — by default (unless disabled above)  
- **Agent-written files** — the SDK **`cwd`** is this directory. Defaults include **`Write`**, **`Edit`**, and **`Bash`** for clones, installs, and generated code.

Re-score a finished run without re-invoking the agent:

- **`npm run score -- --run results/runs/<dir>`** — heuristic (add **`--llm`** for LLM only, **`--write`** to persist sidecars).

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
```

`eval:ci` is **`npm run eval -- --scenarios 01,02`**: both **heuristic** checks and the **LLM judge** (grounded in each scenario’s **`## Success criteria`**). Skipping the judge would leave you with regex-only signal, which does not encode the product checklist.

**GitHub Actions:** add repository secrets **`ANTHROPIC_API_KEY`** and **`EVAL_TEST_DESTINATION_URL`**, run from `docs/agent-evaluation` with a normal runner (Claude Agent SDK needs session filesystem access — avoid tight sandboxes; see **Permissions / failures** above). **`OUTPOST_API_KEY`** is still not required for transcript-only CI.

- **`ANTHROPIC_API_KEY`** — required for the agent and for the **LLM judge** (Success criteria) after each scenario you run.
- **`EVAL_TEST_DESTINATION_URL`** — required for Turn 0; same Source URL as `{{TEST_DESTINATION_URL}}`.
- **`OUTPOST_API_KEY`** — **not** read by the automated runner, but **required if you want a full evaluation**: without it you can only judge the transcript (plausible curl/SDK text). To verify that **generated commands or code actually work**, put the same Outpost API key you use against the managed API in **`docs/agent-evaluation/.env`** (or export it) and run the agent’s output against a real project. The onboarding prompt tells operators to keep that key in **`.env`** and never paste it into chat.
- **`EVAL_LOCAL_DOCS=1`** — before public docs are live, set this so Turn 0 replaces public doc URLs with **absolute paths to MDX/OpenAPI files in this repo** (so the agent should use **Read** on local files instead of WebFetch to production).

- **Turn 0** text is built from [`hookdeck-outpost-agent-prompt.mdx`](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx) (`## Template`) with placeholders filled from environment variables.
- Transcripts are written to `results/runs/<stamp>-scenario-NN/transcript.json` (gitignored).

See `npm run eval -- --help` for env vars (`EVAL_TOOLS`, `EVAL_MODEL`, etc.).

### Permissions / failures (why a run might not work)

Two different things get called “permissions”:

1. **Cursor (or CI) sandbox and `tsx`** — The `tsx` **CLI** opens an IPC pipe in `/tmp` (or similar), which some sandboxes block (`listen EPERM`). This repo’s `npm run eval` uses **`node --import tsx`** instead so Node loads the tsx **loader** only (no CLI IPC). If you still see EPERM, run the same command in a normal terminal outside the sandbox, or use `npm run eval:tsx-cli` only where IPC is allowed.

2. **Claude Agent SDK `dontAsk` + `allowedTools`** — In `dontAsk` mode, tools **not** listed in `allowedTools` are denied (no prompt). Defaults include **`Write`**, **`Edit`**, and **`Bash`** so app scenarios can scaffold and install dependencies inside the per-run directory. With **`EVAL_LOCAL_DOCS=1`**: **`Read,Glob,Grep,Write,Edit,Bash`**. Otherwise **`Read,Glob,Grep,WebFetch,Write,Edit,Bash`**. Narrow **`EVAL_TOOLS`** only if you need a stricter harness (e.g. transcript-only, no shell).

Changing **`EVAL_PERMISSION_MODE`** is usually unnecessary; widening **`EVAL_TOOLS`** (or using local docs) fixes most tool denials.

### Transcript vs execution (full pass)

`npm run eval` only captures **what the model produced**; it does **not** call Outpost. Treat that as **transcript review**.

A **full pass** also answers: *did the generated curl / script / app succeed against a live Outpost project?* Each scenario’s **Success criteria** ends with **Execution** checkboxes for that step. To run them:

1. Add **`OUTPOST_API_KEY`** (and **`OUTPOST_TEST_WEBHOOK_URL`** / **`OUTPOST_API_BASE_URL`** when the artifact expects them) to `docs/agent-evaluation/.env` so your shell has them after `dotenv` or when you `source` / copy into the directory where you run the code.
2. Run the agent’s commands or start its app and complete the flows the scenario describes.
3. Record pass/fail in your run notes ([`results/RUN-RECORDING.template.md`](results/RUN-RECORDING.template.md)).

## Single source of truth for the dashboard prompt

The **full prompt template** (the text operators paste as Turn 0) lives in **one** place:

**[`docs/pages/quickstarts/hookdeck-outpost-agent-prompt.mdx`](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx)** — use the fenced block under **## Template**.

For eval runs, example placeholder substitutions (non-secret) are in [`fixtures/placeholder-values-for-turn0.md`](fixtures/placeholder-values-for-turn0.md) only. That file intentionally **does not** duplicate the template.

The Hookdeck dashboard should eventually render the **same** template body from product-side source; until then, this MDX page is the documentation canonical copy.

## How to run an evaluation (manual)

1. **Turn 0:** Open the [agent prompt MDX](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx), copy **## Template**, replace `{{…}}` (see [placeholder examples](fixtures/placeholder-values-for-turn0.md)).
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
| 09 | `scoreScenario09` | Clone **FastAPI_SAAS_Template** (or git baseline), `outpost_sdk`, integration + domain hook, env key |
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
| 9 | [scenarios/09-integrate-fastapi-existing.md](scenarios/09-integrate-fastapi-existing.md) | **Existing FastAPI SaaS** baseline — Outpost integration ([philipokiokio/FastAPI_SAAS_Template](https://github.com/philipokiokio/FastAPI_SAAS_Template)). |
| 10 | [scenarios/10-integrate-go-existing.md](scenarios/10-integrate-go-existing.md) | **Existing Go SaaS API** baseline — Outpost integration ([devinterface/startersaas-go-api](https://github.com/devinterface/startersaas-go-api)). |

Scenarios **1–4** align with **“Try it out”**; **5–7** with **“Build a minimal example”**; **8–10** with **“Integrate with an existing app”** using pinned OSS baselines (Java / .NET can be added later the same way).

## Agent skills recommendation

**Recommend yes** for teams standardizing on Hookdeck’s skill pack: the [outpost skill](https://github.com/hookdeck/agent-skills/tree/main/skills/outpost) gives agents a consistent overview (tenants, destinations, topics, curl shape) and links into docs.

**Caveats (update the skill in `hookdeck/agent-skills`, not in this repo):**

1. **Managed-first** — The published skill is still **self-hosted heavy** (Docker block first; managed is a short table). For Hookdeck Outpost GA, the skill should foreground [managed quickstarts](../pages/quickstarts/hookdeck-outpost-curl.mdx), `https://api.outpost.hookdeck.com/2025-07-01`, **Settings → Secrets**, and `OUTPOST_API_KEY` / optional `OUTPOST_API_BASE_URL` to match product copy.
2. **REST paths** — Examples must use **`/tenants/{id}`**, not `PUT $BASE_URL/$TENANT_ID` (that path is wrong for the real API).
3. **Naming** — Align env var naming with docs (`OUTPOST_API_KEY` or documented dashboard name), not ad-hoc `HOOKDECK_API_KEY` unless the dashboard literally uses that string.
4. **Router vs. deep skills** — Today `outpost` is one monolithic `SKILL.md`. The skill itself mentions **future** destination-specific skills (`outpost-webhooks`, etc.). For scale, consider either **sections** with clear headings or **child skills** (e.g. `outpost-managed-quickstart`, `outpost-self-hosted`) once content grows—without forcing users to install many tiles for the common case.

Until the skill is updated, agents should still be pointed at the **quickstart MDX pages** in this repo (or production docs URLs); the skill is supplementary.

## Related docs

- [Agent prompt template (SSoT)](../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx)
- [Upstream skill notes](SKILL-UPSTREAM-NOTES.md)
- [TEMP tracking note](../TEMP-hookdeck-outpost-onboarding-status.md)
