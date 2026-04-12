/**
 * Automated Outpost onboarding agent evals via the Claude Agent SDK.
 *
 * Requires ANTHROPIC_API_KEY (and EVAL_TEST_DESTINATION_URL). Does not call Outpost.
 * For a full eval, humans (or a separate verifier) run generated artifacts using OUTPOST_API_KEY — see README.
 *
 * @see https://platform.claude.com/docs/en/agent-sdk/overview
 */

import { writeFileSync } from "node:fs";
import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import { basename, dirname, join, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import dotenv from "dotenv";
import {
  query,
  type HookInput,
  type Options,
  type SDKMessage,
  type SDKSystemMessage,
} from "@anthropic-ai/claude-agent-sdk";
import { applyEvalHarness, parseEvalHarness } from "./eval-harness.js";
import { llmJudgeRun, scenarioMdPathFromRun } from "./llm-judge.js";
import { scoreRunFile } from "./score-transcript.js";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

/** `docs/agent-evaluation/` */
const EVAL_ROOT = join(__dirname, "..");

dotenv.config({ path: join(EVAL_ROOT, ".env") });
/** Outpost repository root */
const REPO_ROOT = join(EVAL_ROOT, "..", "..");
const PROMPT_MDX = join(
  REPO_ROOT,
  "docs/agent-evaluation/hookdeck-outpost-agent-prompt.md",
);
const SCENARIOS_DIR = join(EVAL_ROOT, "scenarios");
const RUNS_DIR = join(EVAL_ROOT, "results", "runs");

/**
 * Harness-only status files next to the run folder (not inside `runDir`) so the agent sandbox cannot Read them.
 * Example: `…/runs/2026-…-scenario-08/transcript.json` vs `…/runs/2026-…-scenario-08.eval-started.json`.
 */
function harnessSidecarPaths(runDir: string): {
  started: string;
  failure: string;
  aborted: string;
} {
  const stem = basename(runDir);
  return {
    started: join(RUNS_DIR, `${stem}.eval-started.json`),
    failure: join(RUNS_DIR, `${stem}.eval-failure.json`),
    aborted: join(RUNS_DIR, `${stem}.eval-aborted.json`),
  };
}

/** Paths for SIGTERM/SIGINT abort sidecar while a scenario is in progress (not SIGKILL). */
let activeHarnessAbortContext: {
  readonly path: string;
  readonly runDirectory: string;
} | null = null;

function registerEvalSignalHandlers(): void {
  const recordAbort = (signal: string) => {
    const ctx = activeHarnessAbortContext;
    if (!ctx) return;
    try {
      writeFileSync(
        ctx.path,
        `${JSON.stringify(
          {
            abortedAt: new Date().toISOString(),
            signal,
            pid: process.pid,
            runDirectory: ctx.runDirectory,
            note: "Process exited before transcript.json was written; long agent turns often print little to stdout.",
          },
          null,
          2,
        )}\n`,
        "utf8",
      );
    } catch {
      // best-effort
    }
  };
  process.once("SIGTERM", () => {
    recordAbort("SIGTERM");
    process.exit(143);
  });
  process.once("SIGINT", () => {
    recordAbort("SIGINT");
    process.exit(130);
  });
}

function isInitSystemMessage(m: SDKMessage): m is SDKSystemMessage {
  return m.type === "system" && m.subtype === "init";
}

function extractTemplateFromMdx(mdx: string): string {
  const idx = mdx.indexOf("## Template");
  if (idx === -1) {
    throw new Error(
      "Could not find ## Template in hookdeck-outpost-agent-prompt.mdoc",
    );
  }
  const after = mdx.slice(idx);
  const fenceStart = after.indexOf("```");
  if (fenceStart === -1) {
    throw new Error("No opening code fence after ## Template");
  }
  const contentStart = after.indexOf("\n", fenceStart) + 1;
  const fenceEnd = after.indexOf("```", contentStart);
  if (fenceEnd === -1) {
    throw new Error("No closing code fence for ## Template");
  }
  return after.slice(contentStart, fenceEnd).trim();
}

function envFlagTruthy(v: string | undefined): boolean {
  if (!v) return false;
  const s = v.trim().toLowerCase();
  return s === "1" || s === "true" || s === "yes";
}

/** Wall-clock heartbeat while the SDK stream is quiet (e.g. long Bash / blocked subprocess). */
function evalProgressIntervalMs(): number {
  const n = Number(process.env.EVAL_PROGRESS_INTERVAL_MS ?? "30000");
  if (!Number.isFinite(n) || n < 5000) {
    return 30000;
  }
  return n;
}

/** When docs are not published yet, point the agent at MDX/OpenAPI paths in this repo. */
function localDocumentationBlock(
  repoRoot: string,
  llmsFullUrl: string | undefined,
): string {
  const f = (...parts: string[]) => join(repoRoot, ...parts);
  const languageSdkBlock = `### Language → SDK vs HTTP

Map what the user says (they rarely name packages):

- **Simplest / minimal / least setup** and no language named → **curl** quickstart + OpenAPI; one shell script; **no SDK**. Read the **entire** curl quickstart (it covers REST responses and any shell portability notes for scripts).
- **TypeScript** or **Node** → TypeScript quickstart + \`@hookdeck/outpost-sdk\` as in that doc.
- **Python** → Python quickstart + \`outpost_sdk\`; \`publish.event(request={{...}})\` as in that doc — not TS-style kwargs.
- **Go** → Go quickstart + official Go SDK as in that doc.
- Explicit **curl** / **HTTP only** / **REST** → curl quickstart + OpenAPI.

**Small app (option 2):** Next.js → TS SDK server-side; FastAPI → Python SDK; Go net/http → Go SDK — use that language’s quickstart for Outpost shapes.

**Existing app (option 3):** Official SDK for the repo’s language (or REST if they refuse SDK).

Do **not** mix TS call shapes into Python.`;

  let block = `### Documentation (local repository — unpublished)

Do **not** rely on live public documentation URLs for this session. Read these files from the Outpost checkout (for example with the **Read** tool). Paths are absolute from the repository root:

Follow **Language → SDK vs HTTP** below for mapping user intent to the **single** right quickstart. Prefer language quickstarts over \`sdks.mdoc\` (TS-heavy).

- **Concepts** (tenants, destinations as subscriptions, topics, how this fits a SaaS/platform): \`${f("docs/content/concepts.mdoc")}\`
- **Building your own UI** (screen structure: list destinations, create flow type → topics → config): \`${f("docs/content/guides/building-your-own-ui.mdoc")}\`
- **Topics** (destination topic subscriptions, fan-out): \`${f("docs/content/features/topics.mdoc")}\`
- Getting started (curl / HTTP only): \`${f("docs/content/quickstarts/hookdeck-outpost-curl.mdoc")}\`
- TypeScript quickstart (TS SDK): \`${f("docs/content/quickstarts/hookdeck-outpost-typescript.mdoc")}\`
- Python quickstart (Python SDK): \`${f("docs/content/quickstarts/hookdeck-outpost-python.mdoc")}\`
- Go quickstart (Go SDK): \`${f("docs/content/quickstarts/hookdeck-outpost-go.mdoc")}\`
- Docs content (browse for feature pages): \`${f("docs/content/")}\`
- OpenAPI spec (machine-readable): \`${f("docs/apis/openapi.yaml")}\`
- **Destination types** (summary + links): \`${f("docs/content/overview.mdoc")}\` — *Supported destinations*; per-type detail in \`docs/content/destinations/*.mdoc\` (e.g. \`${f("docs/content/destinations/webhook.mdoc")}\`)
- SDKs overview (TS-heavy): \`${f("docs/content/sdks.mdoc")}\` — prefer the language quickstart over this for Python/Go/TS code.

${languageSdkBlock}`;
  if (llmsFullUrl) {
    block += `\n- Full docs bundle: ${llmsFullUrl}`;
  }
  return block;
}

function applyPlaceholders(
  template: string,
  env: NodeJS.ProcessEnv,
  repoRoot: string,
): string {
  const apiBase =
    env.EVAL_API_BASE_URL ?? "https://api.outpost.hookdeck.com/2025-07-01";
  const topics = env.EVAL_TOPICS_LIST ?? "- user.created";
  const testUrl = env.EVAL_TEST_DESTINATION_URL?.trim();
  if (!testUrl) {
    throw new Error(
      "Set EVAL_TEST_DESTINATION_URL to your Hookdeck Console Source URL (same value the dashboard injects as {{TEST_DESTINATION_URL}})",
    );
  }
  const docsUrl = env.EVAL_DOCS_URL ?? "https://hookdeck.com/docs/outpost";
  const llms = env.EVAL_LLMS_FULL_URL?.trim() ?? "";
  const useLocalDocs = envFlagTruthy(env.EVAL_LOCAL_DOCS);

  let base = template;
  if (useLocalDocs) {
    const docSection = /^### Documentation\n\n[\s\S]*?(?=\n### What to do\b)/m;
    if (!docSection.test(base)) {
      throw new Error(
        "EVAL_LOCAL_DOCS is set but the prompt template has no ### Documentation section before ### What to do",
      );
    }
    base = base.replace(
      docSection,
      localDocumentationBlock(repoRoot, llms || undefined),
    );
  }

  let out = base
    .replaceAll("{{API_BASE_URL}}", apiBase)
    .replaceAll("{{TOPICS_LIST}}", topics)
    .replaceAll("{{TEST_DESTINATION_URL}}", testUrl)
    .replaceAll("{{DOCS_URL}}", docsUrl)
    .replaceAll("{{LLMS_FULL_URL}}", llms);

  if (!llms) {
    out = out
      .split("\n")
      .filter((line) => !/Full docs bundle/i.test(line))
      .join("\n");
  }

  return out;
}

interface ParsedTurn {
  readonly num: number;
  readonly title: string;
  readonly body: string;
  readonly optional: boolean;
}

function parseScenarioTurns(markdown: string): ParsedTurn[] {
  const lines = markdown.split(/\r?\n/);
  const turns: ParsedTurn[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];
    const m = line.match(/^### Turn (\d+)\s*(.*)$/);
    if (m) {
      const num = Number(m[1]);
      const restOfTitle = m[2] ?? "";
      const title = `Turn ${m[1]}${restOfTitle ? ` ${restOfTitle}` : ""}`;
      const optional = /optional/i.test(title);
      i++;
      const bodyLines: string[] = [];
      while (i < lines.length) {
        const L = lines[i];
        if (/^### /.test(L)) {
          break;
        }
        if (/^## /.test(L)) {
          break;
        }
        bodyLines.push(L);
        i++;
      }
      turns.push({
        num,
        title,
        body: bodyLines.join("\n").trim(),
        optional,
      });
      continue;
    }
    i++;
  }

  return turns.sort((a, b) => a.num - b.num);
}

function extractUserMessage(turnBody: string): string {
  const quoted: string[] = [];
  for (const line of turnBody.split(/\r?\n/)) {
    const q = line.match(/^\s*>\s?(.*)$/);
    if (q) {
      quoted.push(q[1]);
    }
  }
  const fromBlockquote = quoted.join("\n").trim();
  if (fromBlockquote) {
    return fromBlockquote;
  }
  return turnBody.replace(/^\s*$/gm, "").trim();
}

function serializeMessage(message: SDKMessage): unknown {
  try {
    return JSON.parse(
      JSON.stringify(message, (_, v) =>
        typeof v === "bigint" ? v.toString() : v,
      ),
    );
  } catch {
    return { _nonSerializable: String(message) };
  }
}

async function listScenarioFiles(): Promise<string[]> {
  const names = await readdir(SCENARIOS_DIR);
  return names.filter((n) => /^\d{2}-.*\.md$/.test(n)).sort();
}

function idFromFilename(file: string): string {
  return file.slice(0, 2);
}

async function runScenarioQuery(
  prompt: string,
  options: Options,
  progress?: { readonly phaseLabel: string },
): Promise<{ messages: unknown[]; sessionId?: string }> {
  const messages: unknown[] = [];
  let sessionId: string | undefined;
  const progressOn = envFlagTruthy(process.env.EVAL_PROGRESS);
  const label = progress?.phaseLabel ?? "agent query";
  let msgCount = 0;
  let interval: ReturnType<typeof setInterval> | undefined;

  if (progressOn && progress) {
    const maxTurns = options.maxTurns;
    console.error(
      `[eval] ${label}: starting (EVAL_PROGRESS=1; heartbeat every ${evalProgressIntervalMs()}ms; maxTurns=${String(maxTurns)})`,
    );
    interval = setInterval(() => {
      console.error(
        `[eval] ${label}: still running (${msgCount} SDK message(s) so far — subprocess or model may be busy with no new stream events)`,
      );
    }, evalProgressIntervalMs());
  }

  try {
    const q = query({ prompt, options });
    for await (const message of q) {
      msgCount += 1;
      messages.push(serializeMessage(message));
      if (isInitSystemMessage(message)) {
        sessionId = message.session_id;
      }
      if (progressOn && progress && msgCount > 0 && msgCount % 25 === 0) {
        console.error(`[eval] ${label}: ${msgCount} SDK message(s) received`);
      }
    }
  } finally {
    if (interval !== undefined) {
      clearInterval(interval);
    }
  }

  if (progressOn && progress) {
    console.error(
      `[eval] ${label}: finished this query (${msgCount} SDK message(s))`,
    );
  }

  return { messages, sessionId };
}

async function runOneScenario(
  scenarioFile: string,
  filledTemplate: string,
  opts: {
    skipOptional: boolean;
    baseOptions: Options;
    /** When set, avoids a second read of the scenario file (same content as harness parse). */
    scenarioMarkdown?: string;
  },
): Promise<{
  scenarioId: string;
  scenarioFile: string;
  turns: Array<{ label: string; messageCount: number }>;
  sessionId?: string;
  allMessages: unknown[];
}> {
  const path = join(SCENARIOS_DIR, scenarioFile);
  const md = opts.scenarioMarkdown ?? (await readFile(path, "utf8"));
  const parsed = parseScenarioTurns(md);

  const userTurns = parsed
    .filter((t) => t.num >= 1)
    .filter((t) => !t.optional || !opts.skipOptional)
    .map((t) => ({
      label: t.title,
      text: extractUserMessage(t.body),
    }))
    .filter((t) => t.text.length > 0);

  const prompts = [filledTemplate, ...userTurns.map((t) => t.text)];

  const allMessages: unknown[] = [];
  let sessionId: string | undefined;
  const turnStats: Array<{ label: string; messageCount: number }> = [];

  for (let i = 0; i < prompts.length; i++) {
    const label =
      i === 0
        ? "Turn 0 (dashboard prompt)"
        : (userTurns[i - 1]?.label ?? `Turn ${i}`);
    const before = allMessages.length;
    const { messages, sessionId: sid } = await runScenarioQuery(
      prompts[i]!,
      {
        ...opts.baseOptions,
        resume: sessionId,
      },
      { phaseLabel: label },
    );
    if (sid) {
      sessionId = sid;
    }
    allMessages.push(...messages);
    turnStats.push({
      label,
      messageCount: allMessages.length - before,
    });
  }

  return {
    scenarioId: idFromFilename(scenarioFile),
    scenarioFile,
    turns: turnStats,
    sessionId,
    allMessages,
  };
}

/** True if resolved `filePath` is `runDir` or a path inside it (never outside). */
function filePathIsInsideRunDir(runDir: string, filePath: string): boolean {
  const root = resolve(runDir);
  const target = resolve(filePath);
  if (target === root) return true;
  const prefix = root.endsWith(sep) ? root : root + sep;
  return target.startsWith(prefix);
}

function resolveMaybeRelativePath(p: string, agentCwd: string): string {
  if (p.startsWith(sep) || /^[A-Za-z]:[\\/]/.test(p)) {
    return resolve(p);
  }
  return resolve(agentCwd, p);
}

/** Read/Glob/Grep may touch the run directory, or (with local docs) only `repoRoot/docs`. */
function pathAllowedForReadTool(
  absPath: string,
  runDir: string,
  repoRoot: string,
  localDocs: boolean,
): boolean {
  const p = resolve(absPath);
  if (filePathIsInsideRunDir(runDir, p)) return true;
  if (localDocs && filePathIsInsideRunDir(join(repoRoot, "docs"), p))
    return true;
  return false;
}

/**
 * Bash: block commands that reference the Outpost repo root unless the reference stays under
 * `runDir` or (local docs) `repoRoot/docs`.
 */
function bashCommandAllowed(
  command: string,
  runDir: string,
  repoRoot: string,
  localDocs: boolean,
): boolean {
  const rr = resolve(repoRoot);
  const rd = resolve(runDir);
  const docRoot = localDocs ? resolve(join(repoRoot, "docs")) : null;
  if (!command.includes(rr)) return true;
  if (command.includes(rd)) return true;
  if (docRoot && command.includes(docRoot)) return true;
  if (localDocs && command.includes(join(repoRoot, "docs"))) return true;
  return false;
}

function toolInputWritePath(
  toolName: string,
  toolInput: unknown,
): string | undefined {
  if (
    toolName !== "Write" &&
    toolName !== "Edit" &&
    toolName !== "NotebookEdit"
  ) {
    return undefined;
  }
  if (typeof toolInput !== "object" || toolInput === null) return undefined;
  const input = toolInput as Record<string, unknown>;
  for (const k of ["file_path", "path", "notebook_path"] as const) {
    const v = input[k];
    if (typeof v === "string" && v.length > 0) return v;
  }
  return undefined;
}

function toolInputReadFilePath(toolInput: unknown): string | undefined {
  if (typeof toolInput !== "object" || toolInput === null) return undefined;
  const v = (toolInput as Record<string, unknown>).file_path;
  return typeof v === "string" && v.length > 0 ? v : undefined;
}

function preToolDeny(reason: string) {
  return {
    hookSpecificOutput: {
      hookEventName: "PreToolUse" as const,
      permissionDecision: "deny" as const,
      permissionDecisionReason: reason,
    },
  };
}

/**
 * Appended to Turn 0 so the model does not treat the Hookdeck Outpost monorepo as the integration target.
 */
function buildWorkspaceBoundaryAppendix(
  runDir: string,
  agentCwd: string,
  repoRoot: string,
  localDocs: boolean,
): string {
  const docsPath = join(repoRoot, "docs");
  const docBullet = localDocs
    ? `\n- You **may** use Read/Glob/Grep only under **\`${docsPath}\`** when following the **Documentation (local repository)** paths in this prompt—not elsewhere under **\`${repoRoot}\`** (no \`sdks/\`, \`internal/\`, \`go.mod\` at repo root, etc.).`
    : `\n- Do **not** read or search the Hookdeck Outpost checkout on disk outside **\`${runDir}\`**; use the documentation URLs already listed above.`;

  return `

### Workspace boundary (automated eval session)

- The **integration target** is **only** under **\`${runDir}\`** (shell cwd: **\`${agentCwd}\`**). Install dependencies, add SDK usage, routes, UI, and env/README notes **there**.
- Do **not** use Read, Glob, Grep, or Bash to explore **\`${repoRoot}\`** except:${docBullet}
- Do **not** use the **Agent** tool to spider the monorepo or another tree; implement the integration directly in this workspace.
`;
}

/**
 * PreToolUse hook: Write/Edit only under run dir; Read/Glob/Grep/Bash constrained to run dir (+ docs/ when EVAL_LOCAL_DOCS).
 * \`EVAL_DISABLE_WORKSPACE_READ_GUARD=1\` — allow Read/Glob/Grep/Bash/Agent outside the sandbox.
 * \`EVAL_DISABLE_WORKSPACE_WRITE_GUARD=1\` — allow Write/Edit outside the run directory (read sandbox unchanged unless also disabled above).
 */
function createRunDirPreToolHook(ctx: {
  allowedRootDir: string;
  agentCwd: string;
  runDir: string;
  repoRoot: string;
  localDocs: boolean;
  readGuardOn: boolean;
  writeGuardOn: boolean;
}) {
  const {
    allowedRootDir,
    agentCwd,
    runDir,
    repoRoot,
    localDocs,
    readGuardOn,
    writeGuardOn,
  } = ctx;

  return async (input: HookInput) => {
    if (input.hook_event_name !== "PreToolUse") return {};

    if (
      readGuardOn &&
      input.tool_name === "Agent" &&
      !envFlagTruthy(process.env.EVAL_ALLOW_AGENT_TOOL)
    ) {
      return preToolDeny(
        "Outpost agent-eval: the Agent subagent is disabled for fair scoring (set EVAL_ALLOW_AGENT_TOOL=1 to allow).",
      );
    }

    if (readGuardOn && input.tool_name === "Read") {
      const raw = toolInputReadFilePath(input.tool_input);
      if (raw) {
        const abs = resolveMaybeRelativePath(raw, agentCwd);
        if (!pathAllowedForReadTool(abs, runDir, repoRoot, localDocs)) {
          return preToolDeny(
            `Outpost agent-eval: Read must stay under the scenario run directory or (with EVAL_LOCAL_DOCS) ${join(repoRoot, "docs")}. Refused: ${abs}`,
          );
        }
      }
      return {};
    }

    if (readGuardOn && input.tool_name === "Glob") {
      const inp = input.tool_input;
      if (typeof inp === "object" && inp !== null) {
        const pathRaw = (inp as Record<string, unknown>).path;
        if (typeof pathRaw === "string" && pathRaw.length > 0) {
          const abs = resolveMaybeRelativePath(pathRaw, agentCwd);
          if (!pathAllowedForReadTool(abs, runDir, repoRoot, localDocs)) {
            return preToolDeny(
              `Outpost agent-eval: Glob path must stay under the run directory or repo docs/. Refused: ${abs}`,
            );
          }
        }
      }
      return {};
    }

    if (readGuardOn && input.tool_name === "Grep") {
      const inp = input.tool_input;
      if (typeof inp === "object" && inp !== null) {
        const pathRaw = (inp as Record<string, unknown>).path;
        if (typeof pathRaw === "string" && pathRaw.length > 0) {
          const abs = resolveMaybeRelativePath(pathRaw, agentCwd);
          if (!pathAllowedForReadTool(abs, runDir, repoRoot, localDocs)) {
            return preToolDeny(
              `Outpost agent-eval: Grep path must stay under the run directory or repo docs/. Refused: ${abs}`,
            );
          }
        }
      }
      return {};
    }

    if (readGuardOn && input.tool_name === "Bash") {
      const inp = input.tool_input;
      if (typeof inp === "object" && inp !== null) {
        const cmd = (inp as Record<string, unknown>).command;
        if (typeof cmd === "string" && cmd.trim().length > 0) {
          if (!bashCommandAllowed(cmd, runDir, repoRoot, localDocs)) {
            return preToolDeny(
              `Outpost agent-eval: Bash must not traverse the Outpost monorepo outside this run (or docs/ when EVAL_LOCAL_DOCS=1). Refused command prefix: ${cmd.slice(0, 120)}${cmd.length > 120 ? "…" : ""}`,
            );
          }
        }
      }
      return {};
    }

    if (writeGuardOn) {
      const candidate = toolInputWritePath(input.tool_name, input.tool_input);
      if (candidate && !filePathIsInsideRunDir(allowedRootDir, candidate)) {
        return preToolDeny(
          `Outpost agent-eval: ${input.tool_name} must target only the scenario run directory tree. Use a path under ${allowedRootDir}. Refused: ${resolve(candidate)}`,
        );
      }
    }
    return {};
  };
}

function defaultEvalTools(env: NodeJS.ProcessEnv): string {
  if (env.EVAL_TOOLS?.trim()) {
    return env.EVAL_TOOLS.trim();
  }
  // dontAsk + allowedTools: only listed tools are pre-approved; others are denied.
  // Write/Edit: materialize scripts and apps into the per-run directory (agent cwd).
  // Bash: npm/npx/go mod/pip/uv for app scenarios (05–07) and installs for 02–04.
  // WebFetch: omitted when EVAL_LOCAL_DOCS uses repo paths + Read instead.
  return envFlagTruthy(env.EVAL_LOCAL_DOCS)
    ? "Read,Glob,Grep,Write,Edit,Bash"
    : "Read,Glob,Grep,WebFetch,Write,Edit,Bash";
}

function buildBaseOptions(ctx: {
  agentCwd: string;
  writeGuardRoot: string;
  runDir: string;
  repoRoot: string;
  localDocs: boolean;
}): Options {
  const { agentCwd, writeGuardRoot, runDir, repoRoot, localDocs } = ctx;
  const toolsRaw = defaultEvalTools(process.env);
  const allowedTools = toolsRaw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

  const mode = (process.env.EVAL_PERMISSION_MODE ?? "dontAsk") as NonNullable<
    Options["permissionMode"]
  >;

  const maxTurns = Number(process.env.EVAL_MAX_TURNS ?? "80");
  const persistSession = process.env.EVAL_PERSIST_SESSION !== "false";

  const o: Options = {
    cwd: agentCwd,
    allowedTools,
    permissionMode: mode,
    maxTurns: Number.isFinite(maxTurns) ? maxTurns : 80,
    persistSession,
    env: {
      ...process.env,
      CLAUDE_AGENT_SDK_CLIENT_APP: "outpost-docs-agent-eval/1.0.0",
    } as Record<string, string | undefined>,
  };

  const readGuardOn = !envFlagTruthy(
    process.env.EVAL_DISABLE_WORKSPACE_READ_GUARD,
  );
  const writeGuardOn = !envFlagTruthy(
    process.env.EVAL_DISABLE_WORKSPACE_WRITE_GUARD,
  );
  if (readGuardOn || writeGuardOn) {
    o.hooks = {
      PreToolUse: [
        {
          hooks: [
            createRunDirPreToolHook({
              allowedRootDir: writeGuardRoot,
              agentCwd,
              runDir,
              repoRoot,
              localDocs,
              readGuardOn,
              writeGuardOn,
            }),
          ],
        },
      ],
    };
  }

  if (process.env.EVAL_MODEL?.trim()) {
    o.model = process.env.EVAL_MODEL.trim();
  }

  return o;
}

async function main(): Promise<void> {
  const { values } = parseArgs({
    options: {
      scenario: { type: "string" },
      scenarios: { type: "string" },
      all: { type: "boolean", default: false },
      "skip-optional": { type: "boolean", default: false },
      "dry-run": { type: "boolean", default: false },
      "no-score": { type: "boolean", default: false },
      "no-score-llm": { type: "boolean", default: false },
      help: { type: "boolean", short: "h", default: false },
    },
    allowPositionals: false,
  });

  if (values.help) {
    console.log(`
Outpost agent evaluation (Claude Agent SDK)

Usage:
  npm run eval -- --scenario 01
  npm run eval -- --scenarios 01,02,05
  npm run eval -- --all              # deliberate: every scenario (costly)
  npm run eval -- --skip-optional
  npm run eval -- --no-score         # skip heuristic-score.json
  npm run eval -- --no-score-llm     # skip llm-score.json (no Success-criteria judge)
  npm run eval -- --no-score --no-score-llm   # transcripts only
  npm run eval -- --dry-run

You must pass --scenario, --scenarios, or --all so the set of runs is explicit (cost and scope).
After each scenario: transcript + heuristic-score.json + llm-score.json (judge uses ## Success criteria) unless disabled above.
Exit 1 if any enabled score fails.

Environment:
  Values can be set in docs/agent-evaluation/.env (loaded automatically) or exported in the shell.
  ANTHROPIC_API_KEY     Required
  EVAL_TEST_DESTINATION_URL   Required — Hookdeck Console Source URL (fed into {{TEST_DESTINATION_URL}})
  EVAL_API_BASE_URL     Optional (default: managed production URL)
  EVAL_TOPICS_LIST      Optional
  EVAL_DOCS_URL         Optional (ignored for doc links when EVAL_LOCAL_DOCS is set)
  EVAL_LOCAL_DOCS       Set to 1/true/yes to replace Documentation URLs with repo file paths (unpublished docs)
  EVAL_LLMS_FULL_URL    Optional (omit docs line if unset)
  EVAL_TOOLS            Optional, comma-separated (default: Read,Glob,Grep[,WebFetch],Write,Edit,Bash — see README)
  EVAL_MODEL            Optional
  EVAL_MAX_TURNS        Optional (default: 80; npm/go mod installs can exceed 40; lower only for smoke — may not finish 08–10)
  EVAL_PROGRESS         Set to 1/true/yes — log heartbeats to stderr during each agent query (see EVAL_PROGRESS_INTERVAL_MS)
  EVAL_PROGRESS_INTERVAL_MS  Optional (default: 30000, min 5000) — wall-clock heartbeat while the SDK stream is quiet
  EVAL_PERMISSION_MODE  Optional (default: dontAsk)
  EVAL_PERSIST_SESSION  Set to "false" to disable session persistence (breaks multi-turn resume)
  EVAL_DISABLE_WORKSPACE_WRITE_GUARD  Set to 1 to allow Write/Edit outside the run dir (not recommended)
  EVAL_DISABLE_WORKSPACE_READ_GUARD   Set to 1 to allow Read/Glob/Grep/Bash/Agent outside the run dir (+ docs/ when local)
  EVAL_ALLOW_AGENT_TOOL               Set to 1 to allow the Agent subagent (default: denied for fair scoring)
  EVAL_SKIP_HARNESS_PRE_STEPS       Set to 1 to skip ## Eval harness preSteps (git_clone, etc.); see scenario markdown

Outputs under docs/agent-evaluation/results/runs/ (gitignored): each scenario gets
  results/runs/<stamp>-scenario-NN/transcript.json
  heuristic-score.json and llm-score.json unless disabled (see above).
Also set EVAL_NO_SCORE_HEURISTIC=1 or EVAL_NO_SCORE_LLM=1 in .env to skip scoring without flags.

Agent cwd is usually the run directory. Scenarios may define ## Eval harness (JSON) to clone a baseline into a subfolder first.
`);
    process.exit(0);
  }

  if (!process.env.ANTHROPIC_API_KEY?.trim()) {
    console.error("Missing ANTHROPIC_API_KEY");
    process.exit(1);
  }

  const mdx = await readFile(PROMPT_MDX, "utf8");
  const template = extractTemplateFromMdx(mdx);
  const filledTemplate = applyPlaceholders(template, process.env, REPO_ROOT);

  const allFiles = await listScenarioFiles();
  let selected: string[];

  if (values.all) {
    selected = allFiles;
  } else if (values.scenarios) {
    const ids = values.scenarios.split(",").map((s) => s.trim());
    selected = allFiles.filter((f) => ids.includes(idFromFilename(f)));
    const missing = ids.filter(
      (id) => !selected.some((f) => idFromFilename(f) === id),
    );
    if (missing.length) {
      console.error("Unknown scenario id(s):", missing.join(", "));
      process.exit(1);
    }
  } else if (values.scenario) {
    const id = values.scenario.padStart(2, "0");
    selected = allFiles.filter((f) => idFromFilename(f) === id);
    if (selected.length === 0) {
      console.error("Unknown scenario:", values.scenario);
      process.exit(1);
    }
  } else {
    console.error(
      "Choose which scenarios to run (cost is proportional): --scenario <id>, --scenarios id,id, or --all for the full set.",
    );
    console.error(
      `Available: ${allFiles.map((f) => idFromFilename(f)).join(", ")}`,
    );
    process.exit(1);
  }

  if (values["dry-run"]) {
    const localDocs = envFlagTruthy(process.env.EVAL_LOCAL_DOCS);
    const sampleRun = join(RUNS_DIR, "dry-run-example-scenario");
    const sampleAgent = join(sampleRun, "app-baseline");
    const boundarySample = buildWorkspaceBoundaryAppendix(
      sampleRun,
      sampleAgent,
      REPO_ROOT,
      localDocs,
    );
    console.log("Dry run: would execute", selected.join(", "));
    console.log(
      "Turn 0 base template (chars):",
      filledTemplate.length,
      "| + workspace boundary (~chars):",
      boundarySample.length,
    );
    process.exit(0);
  }

  await mkdir(RUNS_DIR, { recursive: true });
  const stamp = new Date().toISOString().replace(/[:.]/g, "-");

  const wantScore =
    !values["no-score"] && !envFlagTruthy(process.env.EVAL_NO_SCORE_HEURISTIC);
  const wantLlm =
    !values["no-score-llm"] && !envFlagTruthy(process.env.EVAL_NO_SCORE_LLM);

  let anyScoreFailure = false;

  console.error(
    `Running ${selected.length} scenario(s): ${selected.join(", ")} (heuristic=${String(wantScore)}, llm=${String(wantLlm)})`,
  );

  registerEvalSignalHandlers();

  for (const file of selected) {
    const scenarioIdEarly = idFromFilename(file);
    const runDir = join(RUNS_DIR, `${stamp}-scenario-${scenarioIdEarly}`);
    await mkdir(runDir, { recursive: true });

    const scenarioPath = join(SCENARIOS_DIR, file);
    const scenarioMd = await readFile(scenarioPath, "utf8");
    const harnessConfig = parseEvalHarness(scenarioMd);
    const { agentCwd, writeGuardRoot } = await applyEvalHarness(
      runDir,
      harnessConfig,
    );
    const localDocs = envFlagTruthy(process.env.EVAL_LOCAL_DOCS);
    const baseOptions = buildBaseOptions({
      agentCwd,
      writeGuardRoot,
      runDir,
      repoRoot: REPO_ROOT,
      localDocs,
    });
    const turn0Prompt =
      filledTemplate +
      buildWorkspaceBoundaryAppendix(runDir, agentCwd, REPO_ROOT, localDocs);
    console.error(
      `\n>>> Scenario ${file} (run dir ${runDir}, agent cwd ${agentCwd}) ...`,
    );
    if (
      scenarioIdEarly === "08" ||
      scenarioIdEarly === "09" ||
      scenarioIdEarly === "10"
    ) {
      console.error(
        "Note: Scenarios 08–10 clone a full baseline and install deps — often 30–90+ min wall time with sparse console output until transcript.json. Ctrl+C aborts (writes *.eval-aborted.json). Set EVAL_PROGRESS=1 for stderr heartbeats. See README § Wall time.",
      );
    }

    const sidecars = harnessSidecarPaths(runDir);
    activeHarnessAbortContext = {
      path: sidecars.aborted,
      runDirectory: runDir,
    };
    await writeFile(
      sidecars.started,
      `${JSON.stringify(
        {
          startedAt: new Date().toISOString(),
          pid: process.pid,
          scenarioFile: file,
          scenarioId: scenarioIdEarly,
          runDirectory: runDir,
          harnessSidecars: {
            started: sidecars.started,
            failure: sidecars.failure,
            aborted: sidecars.aborted,
          },
          note: "Transcript and score JSON live under runDirectory. Harness *.eval-*.json paths are siblings of the run folder (not inside it) so the agent cannot read eval metadata.",
        },
        null,
        2,
      )}\n`,
      "utf8",
    );

    try {
      const result = await runOneScenario(file, turn0Prompt, {
        skipOptional: values["skip-optional"] ?? false,
        baseOptions,
        scenarioMarkdown: scenarioMd,
      });

      const outPath = join(runDir, "transcript.json");
      const payload = {
        meta: {
          scenarioId: result.scenarioId,
          scenarioFile: result.scenarioFile,
          runDirectory: runDir,
          agentWorkspaceCwd: agentCwd,
          evalHarness: {
            preStepCount: harnessConfig.preSteps.length,
            agentCwd: harnessConfig.agentCwd,
          },
          repositoryRoot: REPO_ROOT,
          completedAt: new Date().toISOString(),
          sessionId: result.sessionId,
          turns: result.turns,
        },
        messages: result.allMessages,
      };

      await writeFile(outPath, JSON.stringify(payload, null, 2), "utf8");
      console.error(`Wrote ${outPath}`);

      if (wantScore) {
        const report = await scoreRunFile(outPath);
        const scorePath = join(runDir, "heuristic-score.json");
        await writeFile(
          scorePath,
          `${JSON.stringify(report, null, 2)}\n`,
          "utf8",
        );
        console.error(
          `Wrote ${scorePath} (transcript: ${report.transcript.passed}/${report.transcript.total}, overallTranscriptPass=${String(report.overallTranscriptPass)})`,
        );
        if (report.overallTranscriptPass === false) {
          anyScoreFailure = true;
        }
      }

      if (wantLlm) {
        const scenarioPathForJudge = scenarioMdPathFromRun(
          EVAL_ROOT,
          result.scenarioFile,
        );
        const llmReport = await llmJudgeRun({
          runPath: outPath,
          scenarioMdPath: scenarioPathForJudge,
          apiKey: process.env.ANTHROPIC_API_KEY!.trim(),
        });
        const llmPath = join(runDir, "llm-score.json");
        await writeFile(
          llmPath,
          `${JSON.stringify(llmReport, null, 2)}\n`,
          "utf8",
        );
        console.error(
          `Wrote ${llmPath} (LLM overall_transcript_pass=${String(llmReport.overall_transcript_pass)})`,
        );
        if (!llmReport.overall_transcript_pass) {
          anyScoreFailure = true;
        }
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      const stack = err instanceof Error ? err.stack : undefined;
      await writeFile(
        sidecars.failure,
        `${JSON.stringify({ failedAt: new Date().toISOString(), message, stack, runDirectory: runDir }, null, 2)}\n`,
        "utf8",
      );
      console.error(`Eval scenario failed (${file}):`, err);
      throw err;
    } finally {
      activeHarnessAbortContext = null;
    }
  }

  if (anyScoreFailure) {
    process.exit(1);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
