/**
 * CLI: score a transcript JSON from npm run eval.
 *
 * Usage:
 *   npm run score -- --run results/runs/2026-...-scenario-01.json
 *   npm run score -- --latest
 *   npm run score -- --latest --scenario 01
 *   npm run score -- --run <file>.json --llm --write   # Anthropic judge → .llm-score.json
 */

import { readFile, writeFile } from "node:fs/promises";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import dotenv from "dotenv";
import {
  formatLlmReportHuman,
  llmJudgeRun,
  scenarioMdPathFromRun,
  type LlmJudgeReport,
} from "./llm-judge.js";
import {
  findLatestRunFile,
  formatScoreReportHuman,
  resolveTranscriptJsonPath,
  scoreRunFile,
  scoreSidecarPaths,
  type ScoreReport,
} from "./score-transcript.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const EVAL_ROOT = join(__dirname, "..");
dotenv.config({ path: join(EVAL_ROOT, ".env") });

const RUNS_DIR = join(EVAL_ROOT, "results", "runs");

async function main(): Promise<void> {
  const { values, positionals } = parseArgs({
    options: {
      run: { type: "string" },
      latest: { type: "boolean", default: false },
      scenario: { type: "string" },
      json: { type: "boolean", default: false },
      write: { type: "boolean", default: false },
      llm: { type: "boolean", default: false },
      "no-heuristic": { type: "boolean", default: false },
      help: { type: "boolean", short: "h", default: false },
    },
    allowPositionals: true,
  });

  if (values.help) {
    console.log(`
Score an eval transcript.

  npm run score -- --run results/runs/<stamp>-scenario-01/transcript.json
  npm run score -- --run results/runs/<stamp>-scenario-01   # directory ok
  npm run score -- --latest [--scenario 01]
  npm run score -- --write              # heuristic-score.json + llm-score.json in run dir
  npm run score -- --llm [--write]      # Anthropic judge (needs ANTHROPIC_API_KEY)
  npm run score -- --llm --no-heuristic # LLM only (no regex heuristic)

Heuristic: src/score-transcript.ts. LLM: reads scenarios/*.md Success criteria + assistant text; model from EVAL_SCORE_MODEL (default claude-sonnet-4-20250514).

Options:
  --run <path>      transcript.json, a run directory, or legacy flat *-scenario-NN.json
  --latest          Newest transcript (nested run dir or legacy flat file)
  --scenario <id>   With --latest, filter scenario-0<id>
  --json            Print machine-readable JSON only (last scorer: heuristic or LLM if --llm-only)
  --write           Write sidecar file(s) for enabled scorers
  --llm             Call Anthropic Messages API to judge against Success criteria
  --no-heuristic    Skip regex heuristic (use with --llm for API-only scoring)
`);
    process.exit(0);
  }

  let runPath: string | null = values.run ?? null;
  if (values.latest) {
    runPath = await findLatestRunFile(RUNS_DIR, values.scenario);
    if (!runPath) {
      console.error("No matching run JSON in", RUNS_DIR);
      process.exit(1);
    }
  }

  if (!runPath && positionals[0]) {
    runPath = positionals[0];
  }

  if (!runPath) {
    console.error("Provide --run <path> or --latest");
    process.exit(1);
  }

  let transcriptPath: string;
  try {
    transcriptPath = await resolveTranscriptJsonPath(runPath);
  } catch (e) {
    console.error(String(e));
    process.exit(1);
  }

  const doHeuristic = !values["no-heuristic"];
  const doLlm = values.llm;

  if (!doHeuristic && !doLlm) {
    console.error("Nothing to run: enable heuristic (default) or pass --llm");
    process.exit(1);
  }

  let heuristicReport: ScoreReport | null = null;
  let llmReport: LlmJudgeReport | null = null;
  let fail = false;

  if (doHeuristic) {
    heuristicReport = await scoreRunFile(transcriptPath);
    if (heuristicReport.overallTranscriptPass === false) {
      fail = true;
    }
  }

  if (doLlm) {
    const key = process.env.ANTHROPIC_API_KEY?.trim();
    if (!key) {
      console.error("Missing ANTHROPIC_API_KEY for --llm");
      process.exit(1);
    }
    const raw = await readFile(transcriptPath, "utf8");
    const meta = JSON.parse(raw) as { meta?: { scenarioFile?: string } };
    const scenarioPath = scenarioMdPathFromRun(EVAL_ROOT, meta.meta?.scenarioFile);
    llmReport = await llmJudgeRun({
      runPath: transcriptPath,
      scenarioMdPath: scenarioPath,
      apiKey: key,
    });
    if (!llmReport.overall_transcript_pass) {
      fail = true;
    }
  }

  if (values.json) {
    if (doLlm && values["no-heuristic"]) {
      console.log(JSON.stringify(llmReport, null, 2));
    } else if (doHeuristic && !doLlm) {
      console.log(JSON.stringify(heuristicReport, null, 2));
    } else {
      console.log(
        JSON.stringify({ heuristic: heuristicReport, llm: llmReport }, null, 2),
      );
    }
  } else {
    if (heuristicReport) {
      console.log(formatScoreReportHuman(heuristicReport));
      console.log("");
    }
    if (llmReport) {
      console.log(formatLlmReportHuman(llmReport));
    }
  }

  if (values.write) {
    const { heuristic: heuristicOut, llm: llmOut } = scoreSidecarPaths(transcriptPath);
    if (heuristicReport) {
      await writeFile(heuristicOut, `${JSON.stringify(heuristicReport, null, 2)}\n`, "utf8");
      if (!values.json) {
        console.error(`Wrote ${heuristicOut}`);
      }
    }
    if (llmReport) {
      await writeFile(llmOut, `${JSON.stringify(llmReport, null, 2)}\n`, "utf8");
      if (!values.json) {
        console.error(`Wrote ${llmOut}`);
      }
    }
  }

  process.exit(fail ? 1 : 0);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
