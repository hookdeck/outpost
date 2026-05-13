/**
 * Declarative pre-steps for agent eval scenarios (see `## Eval harness` in scenario markdown).
 */

import { existsSync } from "node:fs";
import { readdir } from "node:fs/promises";
import { join, resolve, sep } from "node:path";

export interface EvalHarnessConfig {
  readonly preSteps: HarnessPreStep[];
  /** Directory under the run folder for the agent process `cwd` (default `"."` = run dir). */
  readonly agentCwd: string;
}

export type HarnessPreStep = GitClonePreStep;

export interface GitClonePreStep {
  readonly type: "git_clone";
  readonly url: string;
  /** Target directory name under the run dir (single segment, no `..`). */
  readonly into: string;
  readonly depth?: number;
  /** If set and `process.env[urlEnv]` is non-empty, use it instead of `url`. */
  readonly urlEnv?: string;
}

const DEFAULT_CONFIG: EvalHarnessConfig = { preSteps: [], agentCwd: "." };

function envFlagTruthy(v: string | undefined): boolean {
  if (!v) return false;
  const s = v.trim().toLowerCase();
  return s === "1" || s === "true" || s === "yes";
}

/** Resolved path must stay under `root` (no `..` escape). */
export function pathMustStayInsideRunDir(root: string, relativeOrAbsolute: string): string {
  const resolved = resolve(relativeOrAbsolute);
  const r = resolve(root);
  if (resolved === r) return resolved;
  const prefix = r.endsWith(sep) ? r : r + sep;
  if (!resolved.startsWith(prefix)) {
    throw new Error(`Path escapes run directory: ${relativeOrAbsolute} -> ${resolved}`);
  }
  return resolved;
}

function assertSingleRunSubdir(name: string, field: string): void {
  if (!name || name === "." || name === "..") {
    throw new Error(`eval-harness: invalid ${field} (empty, ., or ..)`);
  }
  if (name.includes("/") || name.includes("\\") || name.includes("..")) {
    throw new Error(`eval-harness: ${field} must be a single path segment: ${JSON.stringify(name)}`);
  }
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function parseGitCloneStep(raw: Record<string, unknown>, index: number): GitClonePreStep {
  const url = raw.url;
  const into = raw.into;
  if (typeof url !== "string" || url.length === 0) {
    throw new Error(`eval-harness: preSteps[${index}] git_clone requires non-empty string "url"`);
  }
  if (typeof into !== "string" || into.length === 0) {
    throw new Error(`eval-harness: preSteps[${index}] git_clone requires non-empty string "into"`);
  }
  assertSingleRunSubdir(into, "into");
  const depth = raw.depth;
  if (depth !== undefined && (typeof depth !== "number" || !Number.isInteger(depth) || depth < 1)) {
    throw new Error(`eval-harness: preSteps[${index}] git_clone "depth" must be a positive integer`);
  }
  const urlEnv = raw.urlEnv;
  if (urlEnv !== undefined && (typeof urlEnv !== "string" || urlEnv.length === 0)) {
    throw new Error(`eval-harness: preSteps[${index}] git_clone "urlEnv" must be a non-empty string`);
  }
  return {
    type: "git_clone",
    url,
    into,
    ...(depth !== undefined ? { depth } : {}),
    ...(urlEnv ? { urlEnv } : {}),
  };
}

function parsePreStep(raw: unknown, index: number): HarnessPreStep {
  if (!isRecord(raw)) {
    throw new Error(`eval-harness: preSteps[${index}] must be an object`);
  }
  const t = raw.type;
  if (t === "git_clone") {
    return parseGitCloneStep(raw, index);
  }
  throw new Error(`eval-harness: preSteps[${index}] unknown type ${JSON.stringify(t)}`);
}

/**
 * Parse `## Eval harness` and a ```eval-harness JSON block. Missing section → default (no pre-steps, cwd = run dir).
 */
export function parseEvalHarness(markdown: string): EvalHarnessConfig {
  const m = markdown.match(/^## Eval harness\s*$/m);
  if (!m || m.index === undefined) {
    return DEFAULT_CONFIG;
  }
  const afterHeader = markdown.slice(m.index + m[0].length);
  const nextH2 = afterHeader.match(/^## [^\s#]/m);
  const section = nextH2?.index !== undefined ? afterHeader.slice(0, nextH2.index) : afterHeader;
  const fence = section.match(/```eval-harness\s*\n([\s\S]*?)```/);
  if (!fence) {
    throw new Error(
      'Scenario has "## Eval harness" but no ```eval-harness ... ``` JSON block (add one, or remove the heading).',
    );
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(fence[1]!.trim());
  } catch (e) {
    throw new Error(
      `eval-harness: invalid JSON in ## Eval harness block: ${e instanceof Error ? e.message : String(e)}`,
    );
  }
  if (!isRecord(parsed)) {
    throw new Error("eval-harness: root must be a JSON object");
  }
  const preRaw = parsed.preSteps;
  const preSteps: HarnessPreStep[] = [];
  if (preRaw !== undefined) {
    if (!Array.isArray(preRaw)) {
      throw new Error('eval-harness: "preSteps" must be an array');
    }
    for (let i = 0; i < preRaw.length; i++) {
      preSteps.push(parsePreStep(preRaw[i], i));
    }
  }
  let agentCwd = ".";
  const ac = parsed.agentCwd;
  if (ac !== undefined) {
    if (typeof ac !== "string") {
      throw new Error('eval-harness: "agentCwd" must be a string');
    }
    agentCwd = ac.trim() || ".";
  }
  if (agentCwd !== "." && agentCwd !== "") {
    assertSingleRunSubdir(agentCwd, "agentCwd");
  } else {
    agentCwd = ".";
  }
  return { preSteps, agentCwd };
}

async function dirLooksCloned(target: string): Promise<boolean> {
  if (!existsSync(target)) return false;
  const entries = await readdir(target);
  return entries.length > 0;
}

async function runGitClone(runDir: string, step: GitClonePreStep): Promise<void> {
  const url =
    (step.urlEnv && process.env[step.urlEnv]?.trim()) || step.url;
  if (!url) {
    throw new Error(
      `eval-harness: git_clone into ${step.into} has no URL (set "url" or env ${step.urlEnv ?? "(none)"})`,
    );
  }
  const target = join(runDir, step.into);
  if (await dirLooksCloned(target)) {
    console.error(`Harness: skip git_clone (directory already non-empty): ${target}`);
    return;
  }
  const { execFile } = await import("node:child_process");
  const { promisify } = await import("node:util");
  const execFileAsync = promisify(execFile);
  const depth = step.depth ?? 1;
  console.error(`Harness: git clone -> ${target}`);
  try {
    await execFileAsync("git", ["clone", "--depth", String(depth), url, target], {
      cwd: runDir,
      maxBuffer: 50 * 1024 * 1024,
    });
  } catch (err) {
    if (await dirLooksCloned(target)) {
      return;
    }
    throw new Error(
      `Harness git_clone failed (${url} -> ${target}): ${err instanceof Error ? err.message : String(err)}`,
    );
  }
}

/**
 * Run harness pre-steps and return absolute agent cwd + run dir for the write guard.
 */
export async function applyEvalHarness(
  runDir: string,
  config: EvalHarnessConfig,
): Promise<{ agentCwd: string; writeGuardRoot: string }> {
  const writeGuardRoot = runDir;
  const skip = envFlagTruthy(process.env.EVAL_SKIP_HARNESS_PRE_STEPS);

  if (!skip) {
    for (const step of config.preSteps) {
      if (step.type === "git_clone") {
        await runGitClone(runDir, step);
      }
    }
  } else if (config.preSteps.length > 0) {
    console.error("Harness: EVAL_SKIP_HARNESS_PRE_STEPS set — skipped all preSteps.");
  }

  const relative = config.agentCwd === "." ? "" : config.agentCwd;
  const agentCwd = relative ? join(runDir, relative) : runDir;
  pathMustStayInsideRunDir(runDir, agentCwd);

  if (!existsSync(agentCwd)) {
    if (skip) {
      console.error(
        `Harness: agent cwd ${agentCwd} missing (pre-steps skipped); falling back to run dir ${runDir}`,
      );
      return { agentCwd: runDir, writeGuardRoot };
    }
    throw new Error(`Harness: agent cwd does not exist after pre-steps: ${agentCwd}`);
  }

  return { agentCwd, writeGuardRoot };
}
