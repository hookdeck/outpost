/**
 * LLM-as-judge scoring via Anthropic Messages API.
 * Feeds scenario Success criteria + assistant transcript; returns structured JSON from the model.
 */

import { readFile, writeFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";
import { extractTranscriptScoringText } from "./score-transcript.js";
import { redactSecretsForArtifact } from "./redact-secrets.js";

const ANTHROPIC_MESSAGES_URL = "https://api.anthropic.com/v1/messages";
/** Latest Sonnet tier (Feb 2026); override with EVAL_SCORE_MODEL. */
const DEFAULT_SCORE_MODEL = "claude-sonnet-4-6";
const MAX_TRANSCRIPT_CHARS = 180_000;
const MAX_JUDGE_ATTEMPTS = 3;
const JUDGE_MAX_TOKENS = 8192;

interface AnthropicJudgeResponse {
  readonly content?: readonly { type?: string; text?: string }[];
  readonly stop_reason?: string;
  readonly usage?: {
    readonly input_tokens?: number;
    readonly output_tokens?: number;
  };
}

export interface JudgeAttemptDiagnostics {
  readonly attempt: number;
  readonly stop_reason?: string;
  readonly input_tokens?: number;
  readonly output_tokens?: number;
  readonly raw_text_length: number;
  readonly raw_text: string;
  readonly parse_error: string;
}

export interface LlmJudgeFailureArtifact {
  readonly failedAt: string;
  readonly model: string;
  readonly runFile: string;
  readonly attempts: readonly JudgeAttemptDiagnostics[];
}

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

/** When criteria[] is present, overall is the AND of criterion passes (models sometimes disagree). */
export function reconcileOverallTranscriptPass(
  overall_from_model: boolean,
  criteria: readonly LlmCriterionJudgment[],
): boolean {
  if (criteria.length === 0) {
    return overall_from_model;
  }
  return criteria.every((c) => c.pass);
}

/** Evidence phrases that explicitly conclude a criterion IS satisfied. */
const EVIDENCE_CONCLUDES_PASS: readonly RegExp[] = [
  /\bcriterion\s+passes\b/i,
  /\bpasses?\s+based\s+on\b/i,
  /\boverall[^.]{0,60}\bpasses\b/i,
  /\b(?:clearly|ultimately|does|it|this|which)\s+passes\b/i,
  /\bshould\s+pass\b/i,
  /\bcriterion\s+is\s+(?:met|satisfied)\b/i,
];

/** Evidence phrases that explicitly conclude a criterion is NOT satisfied. */
const EVIDENCE_CONCLUDES_FAIL: readonly RegExp[] = [
  /\bcriterion\s+fails\b/i,
  /\bdoes\s+not\s+pass\b/i,
  /\bdid\s+not\s+pass\b/i,
  /\bshould\s+fail\b/i,
  /\b(?:clearly|ultimately|it|this|which)\s+fails\b/i,
  /\bnot\s+(?:met|satisfied)\b/i,
  /\bfails?\s+the\s+criterion\b/i,
];

function anyMatch(patterns: readonly RegExp[], text: string): boolean {
  return patterns.some((re) => re.test(text));
}

/**
 * A criterion self-contradicts when its pass boolean disagrees with an explicit
 * conclusion in its own evidence (e.g. pass=false but evidence says "criterion
 * passes based on live API evidence"). Deliberately conservative: it only fires
 * on explicit pass/fail wording and never when the evidence contains BOTH a
 * pass- and a fail-conclusion (genuinely mixed evidence is left to the model).
 * Used only to trigger a targeted re-ask — never to silently flip a verdict.
 */
export function criterionSelfContradicts(c: LlmCriterionJudgment): boolean {
  const evidence = c.evidence ?? "";
  const concludes_pass = anyMatch(EVIDENCE_CONCLUDES_PASS, evidence);
  const concludes_fail = anyMatch(EVIDENCE_CONCLUDES_FAIL, evidence);
  if (concludes_pass === concludes_fail) {
    return false;
  }
  return c.pass ? concludes_fail : concludes_pass;
}

export function findContradictoryCriteria(
  criteria: readonly LlmCriterionJudgment[],
): readonly LlmCriterionJudgment[] {
  return criteria.filter(criterionSelfContradicts);
}

/** Parse booleans from judge JSON; treats string "true"/"false" (case-insensitive) as explicit. */
export function parseJudgeBooleanExplicit(
  value: unknown,
): { readonly explicit: boolean; readonly value: boolean } {
  if (typeof value === "boolean") {
    return { explicit: true, value };
  }
  if (typeof value === "string") {
    const normalized = value.trim().toLowerCase();
    if (normalized === "true") {
      return { explicit: true, value: true };
    }
    if (normalized === "false") {
      return { explicit: true, value: false };
    }
  }
  return { explicit: false, value: Boolean(value) };
}

function parseJudgeBoolean(value: unknown): boolean {
  return parseJudgeBooleanExplicit(value).value;
}

function parseJudgeJson(text: string): Omit<LlmJudgeReport, "model" | "runFile" | "scenarioFile" | "version"> & {
  version?: number;
} {
  const raw = stripJsonFence(text);
  let parsed: Record<string, unknown>;
  try {
    parsed = JSON.parse(raw) as Record<string, unknown>;
  } catch (parse_err) {
    const detail = parse_err instanceof Error ? parse_err.message : String(parse_err);
    throw new Error(`JSON.parse failed: ${detail}`);
  }
  const overall_parsed = parseJudgeBooleanExplicit(parsed.overall_transcript_pass);
  const overall_from_model = overall_parsed.value;
  const criteriaIn = parsed.criteria;
  const criteria: LlmCriterionJudgment[] = [];
  if (Array.isArray(criteriaIn)) {
    for (const c of criteriaIn) {
      if (typeof c !== "object" || c === null) continue;
      const o = c as Record<string, unknown>;
      criteria.push({
        criterion: String(o.criterion ?? o.id ?? "unnamed"),
        pass: parseJudgeBoolean(o.pass),
        evidence: String(o.evidence ?? ""),
      });
    }
  }
  const overall = reconcileOverallTranscriptPass(overall_from_model, criteria);
  if (criteria.length > 0 && overall_parsed.explicit && overall !== overall_from_model) {
    console.error(
      `LLM judge: reconciled overall_transcript_pass ${overall_from_model} -> ${overall} (${criteria.length} criteria)`,
    );
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

/** True when the eval runner forwarded live Outpost credentials to the agent sandbox. */
export function evalOutpostSecretsAvailable(): boolean {
  return Boolean(process.env.OUTPOST_API_KEY?.trim());
}

const JUDGE_SYSTEM_BASE = `You are an expert evaluator for Hookdeck Outpost onboarding documentation and API usage.
You judge whether an AI assistant's replies satisfy the scenario's Success criteria (markdown checklist from the scenario spec).
Be strict: a criterion passes only if the transcript (including code the model wrote via tools) clearly satisfies it.
You cannot run shell or HTTP — do not claim execution passed; use execution_in_transcript.pass = null and explain in note.
Keep execution_in_transcript.note under 400 characters; put per-criterion detail in criteria[].evidence.
Output ONLY valid JSON (no markdown fences, no commentary outside JSON) matching this shape:
{
  "overall_transcript_pass": boolean,
  "execution_in_transcript": { "pass": null, "note": "string explaining you did not execute code" },
  "criteria": [
    { "criterion": "short label from checklist", "pass": boolean, "evidence": "1-3 sentences; quote or paraphrase assistant" }
  ],
  "summary": "2-4 sentences overall"
}
Map each major bullet/checkbox line from Success criteria to one criteria[] entry (merge tiny sub-bullets if needed).
Each criteria[].pass boolean MUST match your evidence: if your evidence concludes the criterion is satisfied, pass MUST be true; if it concludes the criterion is not satisfied, pass MUST be false. Never emit a criterion whose pass contradicts its own evidence, and never let a caveat or an "I must flag…" aside flip a pass to false when the criterion is otherwise met.
Judge the FINAL state of the deliverable and its LAST relevant run, not the assistant's journey to get there. Coding agents routinely make a mistake and then correct it; a criterion PASSES when the final artifact satisfies it, even if earlier attempts in the same session failed. In particular, do not fail execution- or correctness-style criteria for self-corrected intermediate problems — e.g. a run from the wrong working directory, a module-resolution error (ERR_MODULE_NOT_FOUND), or an argument shape that was fixed — when a later run of the final artifact clearly succeeded (event id printed, expected 2xx/202, exit 0). Only fail when the final deliverable itself is wrong or its last run still fails.`;

function buildHarnessExecutionRules(): string {
  if (evalOutpostSecretsAvailable()) {
    return `Eval-harness / transcript environment: OUTPOST_API_KEY is injected into the agent sandbox for this run (the runner had live Outpost credentials). If the transcript shows the assistant ran curl or SDK smoke tests and they failed (401/403 from bad/missing key in the script, HTTP 4xx/5xx, uncaught exceptions, wrong API usage, mock servers or dummy keys when live keys were available), treat Success-criteria rows about "execution", "runs to completion", or "live API" as FAIL unless the transcript clearly shows success (expected 2xx/202, event id printed, exit 0). Score these rows on the assistant's FINAL script or command and its LAST run: if an earlier attempt failed (wrong working directory, a module-resolution error, or an argument shape that was later fixed) but a subsequent run of the final artifact clearly succeeded, the row PASSES — do not fail it for the earlier, self-corrected attempt. Do not excuse genuine failures as "missing env in the sandbox" — keys were available to the agent. Set overall_transcript_pass false only when a criterion's final state fails under these rules.`;
  }
  return `Eval-harness / transcript environment: The assistant may run Bash (e.g. npx tsx, shell quickstarts) inside an automated eval where live secrets such as OUTPOST_API_KEY are NOT injected (transcript-only / local triage). If the transcript shows the assistant attempted that smoke run and it failed ONLY because required env vars or secrets were missing or empty (clear message: explicit throw, documented "set OUTPOST_API_KEY", 401/403 from missing auth, tool_result text stating unset variable, etc.)—and the written artifacts otherwise match the scenario (SDK usage, endpoints, fail-fast checks, README)—then treat Success-criteria rows about "execution", "runs to completion", or "live API" as PASS for that reason. Keep execution_in_transcript.pass = null (you still did not run code yourself). Set overall_transcript_pass to true when every criteria[] entry passes under these rules; do not fail the whole judgment solely because the eval transcript lacked keys. Do NOT use this exception when the script was never run, the error is vague, or failure likely reflects bugs, syntax errors, wrong API usage, or misconfiguration unrelated to missing env in the sandbox.`;
}

function buildJudgeSystem(): string {
  return `${JUDGE_SYSTEM_BASE}\n\n${buildHarnessExecutionRules()}`;
}

function buildJudgeUserTail(): string {
  if (evalOutpostSecretsAvailable()) {
    return `Judge the transcript against the Success criteria. Remember: execution (running curl or scripts against a live API) is NOT evidenced by you unless the transcript shows successful HTTP/tool outcomes; normally set execution_in_transcript.pass to null. OUTPOST_API_KEY was available in the agent sandbox for this run — score execution-style criteria strictly from what the transcript shows; failed smoke runs, mock servers, or dummy keys are failures unless the transcript shows a clear live success path. Judge the final deliverable and its last run: earlier attempts the assistant fixed before a successful final run do not fail execution-style criteria, and each criterion's pass must match its evidence.`;
  }
  return `Judge the transcript against the Success criteria. Remember: execution (running curl or scripts against a live API) is NOT evidenced by you unless the transcript shows successful HTTP/tool outcomes; normally set execution_in_transcript.pass to null. If the transcript shows a run attempt failed only because OUTPOST_API_KEY or other required env was missing in the eval sandbox, apply the harness exception in your system instructions for execution-style criteria—do not mark overall_transcript_pass false for that alone.`;
}

function judgeFailureArtifactPath(run_path: string): string {
  return join(dirname(run_path), "llm-judge-failure.json");
}

async function writeJudgeFailureArtifact(
  run_path: string,
  artifact: LlmJudgeFailureArtifact,
): Promise<string> {
  const path = judgeFailureArtifactPath(run_path);
  const redacted: LlmJudgeFailureArtifact = {
    ...artifact,
    attempts: artifact.attempts.map((a) => ({
      ...a,
      raw_text: redactSecretsForArtifact(a.raw_text),
    })),
  };
  await writeFile(path, `${JSON.stringify(redacted, null, 2)}\n`, "utf8");
  return path;
}

function logJudgeAttempt(
  attempt: number,
  max_attempts: number,
  api_body: AnthropicJudgeResponse,
  raw_text: string,
): void {
  const usage = api_body.usage;
  console.error(
    `LLM judge attempt ${attempt}/${max_attempts}: stop_reason=${api_body.stop_reason ?? "unknown"} ` +
      `input_tokens=${usage?.input_tokens ?? "?"} output_tokens=${usage?.output_tokens ?? "?"} ` +
      `raw_chars=${raw_text.length}`,
  );
}

async function callAnthropicJudge(options: {
  readonly api_key: string;
  readonly model: string;
  readonly system: string;
  readonly user_content: string;
  readonly retry_note?: string;
}): Promise<{ readonly text: string; readonly body: AnthropicJudgeResponse }> {
  const user_content = options.retry_note
    ? `${options.user_content}\n\n---\n\n${options.retry_note}`
    : options.user_content;

  const res = await fetch(ANTHROPIC_MESSAGES_URL, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "x-api-key": options.api_key,
      "anthropic-version": "2023-06-01",
    },
    body: JSON.stringify({
      model: options.model,
      max_tokens: JUDGE_MAX_TOKENS,
      system: options.system,
      messages: [{ role: "user", content: user_content }],
    }),
  });

  if (!res.ok) {
    const err_text = await res.text();
    throw new Error(`Anthropic API ${res.status}: ${err_text.slice(0, 2000)}`);
  }

  const body = (await res.json()) as AnthropicJudgeResponse;
  const text_block = body.content?.find((c) => c.type === "text");
  const text = text_block?.text ?? "";
  return { text, body };
}

const JSON_RETRY_NOTE =
  `IMPORTANT: Your previous response was not valid complete JSON (see prior attempt diagnostics). ` +
  `Output ONLY a single complete JSON object matching the schema in your system instructions — ` +
  `no markdown fences, no commentary. Ensure the response ends with closing braces and includes summary.`;

/** Re-ask note listing criteria whose pass boolean disagreed with their own evidence. */
function buildContradictionRetryNote(
  contradictions: readonly LlmCriterionJudgment[],
): string {
  const list = contradictions
    .map((c) => `- "${c.criterion}" (you set pass=${c.pass}): ${c.evidence}`)
    .join("\n");
  return (
    `IMPORTANT: In your previous response these criteria set a pass boolean that contradicts their own evidence:\n${list}\n\n` +
    `Re-judge using the rules in your system instructions: judge the FINAL state of the deliverable and its last run, do not fail a criterion for self-corrected intermediate failures, and make each criterion's pass match its evidence. ` +
    `Output ONLY the complete JSON object again with corrected pass values and an overall_transcript_pass consistent with them.`
  );
}

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

${buildJudgeUserTail()}`;

  const system = buildJudgeSystem();
  const attempt_diagnostics: JudgeAttemptDiagnostics[] = [];
  let judged: ReturnType<typeof parseJudgeJson> | undefined;
  let retry_note: string | undefined;

  for (let attempt = 1; attempt <= MAX_JUDGE_ATTEMPTS; attempt++) {
    const { text, body } = await callAnthropicJudge({
      api_key: options.apiKey,
      model,
      system,
      user_content: userContent,
      retry_note,
    });

    logJudgeAttempt(attempt, MAX_JUDGE_ATTEMPTS, body, text);

    try {
      const parsed = parseJudgeJson(text);
      const contradictions = findContradictoryCriteria(parsed.criteria);
      if (contradictions.length > 0 && attempt < MAX_JUDGE_ATTEMPTS) {
        // Keep this parse as a fallback, then re-ask the model to reconcile
        // pass with evidence rather than silently flipping the verdict.
        judged = parsed;
        retry_note = buildContradictionRetryNote(contradictions);
        console.error(
          `LLM judge attempt ${attempt}: ${contradictions.length} criterion(s) where pass disagrees with evidence ` +
            `(${contradictions.map((c) => c.criterion).join(", ")}); re-asking to reconcile…`,
        );
        continue;
      }
      judged = parsed;
      break;
    } catch (parse_err) {
      const parse_error =
        parse_err instanceof Error ? parse_err.message : String(parse_err);
      attempt_diagnostics.push({
        attempt,
        stop_reason: body.stop_reason,
        input_tokens: body.usage?.input_tokens,
        output_tokens: body.usage?.output_tokens,
        raw_text_length: text.length,
        raw_text: text,
        parse_error,
      });
      retry_note = JSON_RETRY_NOTE;
      if (attempt < MAX_JUDGE_ATTEMPTS) {
        console.error(
          `LLM judge attempt ${attempt} parse failed (${parse_error}); retrying…`,
        );
      }
    }
  }

  if (!judged) {
    const last = attempt_diagnostics[attempt_diagnostics.length - 1];
    const failure_artifact: LlmJudgeFailureArtifact = {
      failedAt: new Date().toISOString(),
      model,
      runFile: options.runPath,
      attempts: attempt_diagnostics,
    };
    const artifact_path = await writeJudgeFailureArtifact(options.runPath, failure_artifact);
    console.error(`Wrote ${artifact_path} (full judge raw responses from ${attempt_diagnostics.length} attempts)`);

    throw new Error(
      `Judge did not return parseable JSON after ${MAX_JUDGE_ATTEMPTS} attempts. ` +
        `Last stop_reason=${last?.stop_reason ?? "unknown"} ` +
        `output_tokens=${last?.output_tokens ?? "?"} raw_chars=${last?.raw_text_length ?? 0}. ` +
        `Full responses: ${artifact_path}. First 800 chars of last attempt:\n${(last?.raw_text ?? "").slice(0, 800)}`,
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
