/**
 * LLM-as-judge scoring via Anthropic Messages API.
 * Feeds scenario Success criteria + assistant transcript; returns structured JSON from the model.
 */

import { readFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";
import { extractTranscriptScoringText } from "./score-transcript.js";

const ANTHROPIC_MESSAGES_URL = "https://api.anthropic.com/v1/messages";
const DEFAULT_SCORE_MODEL = "claude-sonnet-4-20250514";
const MAX_TRANSCRIPT_CHARS = 180_000;

export interface LlmCriterionJudgment {
  readonly criterion: string;
  readonly pass: boolean;
  readonly evidence: string;
}

export interface LlmJudgeReport {
  readonly version: 1;
  readonly model: string;
  readonly runFile: string;
  readonly scenarioFile: string;
  readonly overall_transcript_pass: boolean;
  /** LLM cannot run curls; always note limits */
  readonly execution_in_transcript: {
    readonly pass: boolean | null;
    readonly note: string;
  };
  readonly criteria: readonly LlmCriterionJudgment[];
  readonly summary: string;
}

interface RunJson {
  meta?: {
    scenarioId?: string;
    scenarioFile?: string;
    turns?: readonly { label?: string; messageCount?: number }[];
  };
  messages?: unknown[];
}

export function extractSuccessCriteriaMarkdown(fullMd: string): string {
  const anchor = "## Success criteria";
  const i = fullMd.indexOf(anchor);
  if (i === -1) {
    return "(No ## Success criteria section found.)";
  }
  const rest = fullMd.slice(i);
  const sub = rest.slice(anchor.length);
  const rel = sub.search(/\n## [A-Za-z]/);
  return rel === -1 ? rest.trim() : rest.slice(0, anchor.length + rel).trim();
}

function stripJsonFence(text: string): string {
  const t = text.trim();
  const m = t.match(/^```(?:json)?\s*([\s\S]*?)```$/m);
  if (m) return m[1].trim();
  return t;
}

function parseJudgeJson(text: string): Omit<LlmJudgeReport, "model" | "runFile" | "scenarioFile" | "version"> & {
  version?: number;
} {
  const raw = stripJsonFence(text);
  const parsed = JSON.parse(raw) as Record<string, unknown>;
  const overall = Boolean(parsed.overall_transcript_pass);
  const criteriaIn = parsed.criteria;
  const criteria: LlmCriterionJudgment[] = [];
  if (Array.isArray(criteriaIn)) {
    for (const c of criteriaIn) {
      if (typeof c !== "object" || c === null) continue;
      const o = c as Record<string, unknown>;
      criteria.push({
        criterion: String(o.criterion ?? o.id ?? "unnamed"),
        pass: Boolean(o.pass),
        evidence: String(o.evidence ?? ""),
      });
    }
  }
  const exec = parsed.execution_in_transcript;
  let execution_in_transcript: LlmJudgeReport["execution_in_transcript"] = {
    pass: null,
    note: "Not specified by judge.",
  };
  if (typeof exec === "object" && exec !== null) {
    const e = exec as Record<string, unknown>;
    execution_in_transcript = {
      pass: typeof e.pass === "boolean" ? e.pass : null,
      note: String(e.note ?? ""),
    };
  }
  return {
    overall_transcript_pass: overall,
    execution_in_transcript: execution_in_transcript,
    criteria,
    summary: String(parsed.summary ?? ""),
  };
}

const JUDGE_SYSTEM = `You are an expert evaluator for Hookdeck Outpost onboarding documentation and API usage.
You judge whether an AI assistant's replies satisfy the scenario's Success criteria (markdown checklist from the scenario spec).
Be strict: a criterion passes only if the transcript (including code the model wrote via tools) clearly satisfies it.
You cannot run shell or HTTP — do not claim execution passed; use execution_in_transcript.pass = null and explain in note.
Output ONLY valid JSON (no markdown fences, no commentary outside JSON) matching this shape:
{
  "overall_transcript_pass": boolean,
  "execution_in_transcript": { "pass": null, "note": "string explaining you did not execute code" },
  "criteria": [
    { "criterion": "short label from checklist", "pass": boolean, "evidence": "1-3 sentences; quote or paraphrase assistant" }
  ],
  "summary": "2-4 sentences overall"
}
Map each major bullet/checkbox line from Success criteria to one criteria[] entry (merge tiny sub-bullets if needed).`;

export async function llmJudgeRun(options: {
  readonly runPath: string;
  readonly scenarioMdPath: string;
  readonly apiKey: string;
  readonly model?: string;
}): Promise<LlmJudgeReport> {
  const model = options.model?.trim() || process.env.EVAL_SCORE_MODEL?.trim() || DEFAULT_SCORE_MODEL;
  const rawRun = await readFile(options.runPath, "utf8");
  const data = JSON.parse(rawRun) as RunJson;
  const scenarioFile = data.meta?.scenarioFile ?? "unknown.md";
  const scenarioMd = await readFile(options.scenarioMdPath, "utf8");
  const criteriaBlock = extractSuccessCriteriaMarkdown(scenarioMd);

  let transcript = extractTranscriptScoringText(data.messages);
  if (transcript.length > MAX_TRANSCRIPT_CHARS) {
    transcript =
      transcript.slice(0, MAX_TRANSCRIPT_CHARS) +
      "\n\n[… transcript truncated for judge context …]\n";
  }

  const userContent = `## Success criteria (from scenario spec — your rubric)

${criteriaBlock}

---

## Transcript for review (assistant text plus tool-written file contents and tool inputs from the run JSON)

${transcript}

---

Judge the transcript against the Success criteria. Remember: execution (running curl against a live API) is NOT evidenced here unless the transcript explicitly describes successful HTTP results; normally set execution_in_transcript.pass to null.`;

  const res = await fetch(ANTHROPIC_MESSAGES_URL, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "x-api-key": options.apiKey,
      "anthropic-version": "2023-06-01",
    },
    body: JSON.stringify({
      model,
      max_tokens: 8192,
      system: JUDGE_SYSTEM,
      messages: [{ role: "user", content: userContent }],
    }),
  });

  if (!res.ok) {
    const errText = await res.text();
    throw new Error(`Anthropic API ${res.status}: ${errText.slice(0, 2000)}`);
  }

  const body = (await res.json()) as {
    content?: readonly { type?: string; text?: string }[];
  };
  const textBlock = body.content?.find((c) => c.type === "text");
  const text = textBlock?.text ?? "";
  let judged: ReturnType<typeof parseJudgeJson>;
  try {
    judged = parseJudgeJson(text);
  } catch {
    throw new Error(
      `Judge did not return parseable JSON. First 800 chars:\n${text.slice(0, 800)}`,
    );
  }

  return {
    version: 1,
    model,
    runFile: options.runPath,
    scenarioFile,
    overall_transcript_pass: judged.overall_transcript_pass,
    execution_in_transcript: judged.execution_in_transcript,
    criteria: judged.criteria,
    summary: judged.summary,
  };
}

export function scenarioMdPathFromRun(
  evalRoot: string,
  scenarioFile: string | undefined,
): string {
  if (!scenarioFile?.trim()) {
    throw new Error("Run JSON meta.scenarioFile is missing");
  }
  return join(evalRoot, "scenarios", scenarioFile);
}

export function formatLlmReportHuman(r: LlmJudgeReport): string {
  const lines: string[] = [
    `LLM judge (${r.model})`,
    `Transcript: ${r.runFile}`,
    `Scenario: ${r.scenarioFile}`,
  ];
  if (basename(r.runFile) === "transcript.json") {
    lines.push(`Run directory: ${dirname(r.runFile)}`);
  }
  lines.push(
    "",
    `Overall transcript pass: ${r.overall_transcript_pass ? "YES" : "NO"}`,
    `Execution (from transcript only): pass=${String(r.execution_in_transcript.pass)} — ${r.execution_in_transcript.note}`,
    "",
    "Per criterion:",
  );
  for (const c of r.criteria) {
    lines.push(`  [${c.pass ? "PASS" : "FAIL"}] ${c.criterion}`);
    lines.push(`         ${c.evidence}`);
  }
  lines.push("");
  lines.push(`Summary: ${r.summary}`);
  return lines.join("\n");
}
