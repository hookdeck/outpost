/**
 * Extract ordered trajectory steps from eval transcript.json for visualization.
 * Message shapes align with src/score-transcript.ts (assistant tool_use, user tool_result).
 */

export type TrajectoryKind =
  | "read"
  | "fetch"
  | "write"
  | "bash"
  | "search"
  | "other";

export interface TrajectoryStep {
  readonly stepIndex: number;
  /** Index into transcript `messages` where this tool_use appeared */
  readonly messageIndex: number;
  readonly turnLabel: string;
  readonly toolUseId: string;
  readonly toolName: string;
  readonly kind: TrajectoryKind;
  /** Doc path, URL, file path, or empty */
  readonly target: string;
  /** Truncated command, extra context, or empty */
  readonly detail: string;
  readonly tags: readonly string[];
  /** Heuristic SDK / API tokens spotted in tool input (Bash/Write/Edit) */
  readonly sdkHints: readonly string[];
  /**
   * Read tool only: prose/docs extensions vs source/config reads.
   * `.md`, `.mdx`, `.mdoc`, `.rst` → documentation; other paths → code.
   */
  readonly readMaterial: "documentation" | "code" | null;
  /**
   * Read + WebFetch: where this path/URL sits in the doc graph (heuristic).
   * Use for filtering “reference docs”, OpenAPI, quickstarts, etc.
   */
  readonly docSignals: readonly string[];
  readonly isError: boolean;
  readonly resultPreview: string;
}

export interface TurnBoundary {
  readonly label: string;
  readonly startMessageIndex: number;
  readonly endMessageIndex: number;
}

export interface RunMetaSummary {
  readonly scenarioId?: string;
  readonly scenarioFile?: string;
  readonly runDirectory?: string;
  readonly sessionId?: string;
  readonly completedAt?: string;
  readonly model?: string;
}

export interface HeuristicCheckSummary {
  readonly id: string;
  readonly pass: boolean;
  readonly detail: string;
}

export interface TrajectoryPayload {
  readonly meta: RunMetaSummary;
  readonly turns: readonly TurnBoundary[];
  readonly steps: readonly TrajectoryStep[];
  readonly heuristicChecks?: readonly HeuristicCheckSummary[];
  readonly overallTranscriptPass?: boolean | null;
  readonly llmOverallPass?: boolean | null;
}

interface RunJson {
  meta?: {
    scenarioId?: string;
    scenarioFile?: string;
    runDirectory?: string;
    sessionId?: string;
    completedAt?: string;
    turns?: readonly { label?: string; messageCount?: number }[];
  };
  messages?: unknown[];
}

function isRecord(x: unknown): x is Record<string, unknown> {
  return typeof x === "object" && x !== null;
}

function redactSecrets(text: string, maxLen: number): string {
  let s = text;
  s = s.replace(/Bearer\s+sk-ant-api[^\s"'`]+/gi, "Bearer [REDACTED]");
  s = s.replace(/Bearer\s+[A-Za-z0-9_-]{40,}/g, "Bearer [REDACTED]");
  if (s.length > maxLen) s = `${s.slice(0, maxLen)}…`;
  return s;
}

function summarizeToolResultContent(content: unknown): string {
  if (typeof content === "string") {
    return redactSecrets(content.trim().replace(/\s+/g, " "), 400);
  }
  if (!Array.isArray(content)) return "";
  const parts: string[] = [];
  for (const item of content) {
    if (!isRecord(item)) continue;
    if (item.type === "text" && typeof item.text === "string") {
      parts.push(item.text);
    }
  }
  return redactSecrets(parts.join(" ").trim().replace(/\s+/g, " "), 400);
}

export function buildTurnBoundaries(meta: RunJson["meta"]): TurnBoundary[] {
  const turns = meta?.turns ?? [];
  const out: TurnBoundary[] = [];
  let start = 0;
  for (const t of turns) {
    const n = Number(t.messageCount);
    const count = Number.isFinite(n) && n > 0 ? n : 0;
    if (count === 0) continue;
    const label =
      typeof t.label === "string" && t.label.length > 0 ? t.label : "Turn";
    const end = start + count - 1;
    out.push({ label, startMessageIndex: start, endMessageIndex: end });
    start += count;
  }
  return out;
}

function turnLabelForMessageIndex(
  boundaries: readonly TurnBoundary[],
  messageIndex: number,
): string {
  for (const b of boundaries) {
    if (
      messageIndex >= b.startMessageIndex &&
      messageIndex <= b.endMessageIndex
    ) {
      return b.label;
    }
  }
  return "(unknown turn)";
}

/** Path or URL looks like prose documentation (local or URL). */
const READ_DOC_EXT = /\.(md|mdx|mdoc|rst)$/i;

export function readMaterialFromPath(filePath: string): "documentation" | "code" {
  const t = filePath.trim();
  if (!t) return "code";
  return READ_DOC_EXT.test(t) ? "documentation" : "code";
}

/**
 * Heuristic doc buckets for Read targets and WebFetch URLs (Outpost eval context).
 */
export function inferDocSignals(
  pathOrUrl: string,
  kind: TrajectoryKind,
): string[] {
  const raw = pathOrUrl.trim();
  if (!raw) return [];
  const u = raw.toLowerCase();
  const out: string[] = [];

  if (kind === "fetch" || /hookdeck\.com\/docs/i.test(raw)) {
    out.push("published-docs");
  }
  if (/\/references\/|\/pages\/references\//i.test(raw)) {
    out.push("reference");
  }
  if (/openapi\.ya?ml(\.|$)/i.test(raw) || /[/\\]apis[/\\][^/\\]*\.ya?ml$/i.test(raw)) {
    out.push("openapi");
  }
  if (/quickstarts?[/\\]/i.test(u) || /hookdeck-outpost-[^/\\]*\.(mdoc|mdx)\b/i.test(u)) {
    out.push("quickstart");
  }
  if (/[/\\]concepts\.(mdoc|mdx)\b/i.test(raw)) {
    out.push("concepts");
  }
  if (/[/\\]docs[/\\]content[/\\]/i.test(raw) && /\.mdoc\b/i.test(raw)) {
    out.push("content-doc");
  }
  if (/[/\\]docs[/\\]pages[/\\]/i.test(raw) && /\.mdx\b/i.test(raw)) {
    out.push("pages-doc");
  }
  if (/[/\\]sdks[/\\]/i.test(raw) && /outpost|hookdeck/i.test(raw)) {
    out.push("sdk-source");
  }
  if (/[/\\]destinations[/\\][^/\\]*\.(mdoc|mdx)\b/i.test(raw)) {
    out.push("destination-doc");
  }
  if (READ_DOC_EXT.test(raw) && out.length === 0 && kind === "read") {
    out.push("prose-file");
  }

  return [...new Set(out)];
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
  if (!isRecord(toolInput)) return undefined;
  for (const k of ["file_path", "path", "notebook_path"] as const) {
    const v = toolInput[k];
    if (typeof v === "string" && v.length > 0) return v;
  }
  return undefined;
}

const SDK_HINT_PATTERNS: ReadonlyArray<{ id: string; re: RegExp }> = [
  { id: "ts_publish.event", re: /\bpublish\.event\b/i },
  { id: "ts_tenants.upsert", re: /\btenants\.upsert\b/i },
  { id: "ts_destinations.create", re: /\bdestinations\.create\b/i },
  { id: "py_publish", re: /\bpublish\.event\b/i },
  { id: "go_Publish.Event", re: /\bPublish\.Event\b/i },
  { id: "go_Tenants.Upsert", re: /\bTenants\.Upsert\b/i },
  { id: "go_CreateDestinationCreateWebhook", re: /\bCreateDestinationCreateWebhook\b/i },
];

function extractSdkHints(toolName: string, input: unknown): string[] {
  if (
    toolName !== "Bash" &&
    toolName !== "Write" &&
    toolName !== "Edit" &&
    toolName !== "NotebookEdit"
  ) {
    return [];
  }
  let corpus = "";
  try {
    corpus = JSON.stringify(input);
  } catch {
    corpus = "";
  }
  const hints: string[] = [];
  for (const { id, re } of SDK_HINT_PATTERNS) {
    if (re.test(corpus)) hints.push(id);
  }
  return hints;
}

function classifyStep(
  toolName: string,
  input: unknown,
): {
  kind: TrajectoryKind;
  target: string;
  detail: string;
  tags: string[];
} {
  const tags: string[] = [];
  if (toolName === "Read") {
    const fp =
      isRecord(input) && typeof input.file_path === "string"
        ? input.file_path
        : "";
    return { kind: "read", target: fp, detail: "", tags };
  }
  if (toolName === "WebFetch") {
    const url =
      isRecord(input) && typeof input.url === "string" ? input.url : "";
    const prompt =
      isRecord(input) && typeof input.prompt === "string"
        ? redactSecrets(input.prompt.replace(/\s+/g, " "), 200)
        : "";
    return { kind: "fetch", target: url, detail: prompt, tags };
  }
  if (
    toolName === "Write" ||
    toolName === "Edit" ||
    toolName === "NotebookEdit"
  ) {
    const p = toolInputWritePath(toolName, input) ?? "";
    return { kind: "write", target: p, detail: toolName, tags };
  }
  if (toolName === "Bash") {
    const cmd =
      isRecord(input) && typeof input.command === "string" ? input.command : "";
    const d = redactSecrets(cmd.replace(/\s+/g, " "), 500);
    if (/\bcurl\b|wget|https?:\/\//i.test(cmd)) tags.push("http-like");
    if (
      /api\.outpost\.hookdeck\.com|\/tenants\/|\/destinations|\/publish\b/i.test(
        cmd,
      )
    ) {
      tags.push("outpost-api-ish");
    }
    if (/localhost:3333|\/api\/v1\b/i.test(cmd)) tags.push("self-hosted-ish");
    return { kind: "bash", target: "", detail: d, tags };
  }
  if (toolName === "Glob" || toolName === "Grep" || toolName === "ToolSearch") {
    let bits = "";
    if (isRecord(input)) {
      if (typeof input.pattern === "string") bits += input.pattern.slice(0, 80);
      if (typeof input.path === "string")
        bits += (bits ? " @ " : "") + input.path.slice(0, 120);
    }
    return { kind: "search", target: bits, detail: toolName, tags };
  }
  let json = "";
  try {
    json = JSON.stringify(input);
  } catch {
    json = "";
  }
  return {
    kind: "other",
    target: "",
    detail: redactSecrets(json, 300),
    tags,
  };
}

function findToolResult(
  messages: unknown[],
  afterMessageIndex: number,
  toolUseId: string,
): { isError: boolean; preview: string } | null {
  for (let i = afterMessageIndex + 1; i < messages.length; i++) {
    const m = messages[i];
    if (!isRecord(m) || m.type !== "user") continue;
    const inner = m.message;
    if (!isRecord(inner)) continue;
    const content = inner.content;
    if (!Array.isArray(content)) continue;
    for (const block of content) {
      if (!isRecord(block) || block.type !== "tool_result") continue;
      if (String(block.tool_use_id ?? "") !== toolUseId) continue;
      return {
        isError: Boolean(block.is_error),
        preview: summarizeToolResultContent(block.content),
      };
    }
  }
  return null;
}

function initModelFromMessages(messages: unknown[]): string | undefined {
  for (const m of messages) {
    if (!isRecord(m)) continue;
    if (
      m.type === "system" &&
      m.subtype === "init" &&
      typeof m.model === "string"
    ) {
      return m.model;
    }
  }
  return undefined;
}

export function extractRunTrajectory(
  raw: RunJson,
  scoreSide?: {
    readonly heuristicChecks?: readonly HeuristicCheckSummary[];
    readonly overallTranscriptPass?: boolean | null;
    readonly llmOverallPass?: boolean | null;
  },
): TrajectoryPayload {
  const messages = raw.messages ?? [];
  const boundaries = buildTurnBoundaries(raw.meta);
  const meta: RunMetaSummary = {
    scenarioId: raw.meta?.scenarioId,
    scenarioFile: raw.meta?.scenarioFile,
    runDirectory: raw.meta?.runDirectory,
    sessionId: raw.meta?.sessionId,
    completedAt: raw.meta?.completedAt,
    model: initModelFromMessages(messages),
  };

  const steps: TrajectoryStep[] = [];
  let stepIndex = 0;

  for (let messageIndex = 0; messageIndex < messages.length; messageIndex++) {
    const m = messages[messageIndex];
    if (!isRecord(m) || m.type !== "assistant") continue;
    const inner = m.message;
    if (!isRecord(inner)) continue;
    const content = inner.content;
    if (!Array.isArray(content)) continue;

    for (const block of content) {
      if (!isRecord(block) || block.type !== "tool_use") continue;
      const toolUseId = String(block.id ?? "");
      const toolName = String(block.name ?? "?");
      const input = block.input;
      const { kind, target, detail, tags } = classifyStep(toolName, input);
      const sdkHints = extractSdkHints(toolName, input);
      const res = findToolResult(messages, messageIndex, toolUseId);
      const mergedTags = [...tags];
      if (
        kind === "read" &&
        /sdks\.mdoc|self-hosted|localhost:3333/i.test(target)
      ) {
        mergedTags.push("maybe-off-topic");
      }

      const readMaterial =
        kind === "read" ? readMaterialFromPath(target) : null;
      const docSignals =
        kind === "read" || kind === "fetch"
          ? inferDocSignals(target, kind)
          : [];

      steps.push({
        stepIndex: stepIndex++,
        messageIndex,
        turnLabel: turnLabelForMessageIndex(boundaries, messageIndex),
        toolUseId,
        toolName,
        kind,
        target: redactSecrets(target, 500),
        detail,
        tags: mergedTags,
        sdkHints,
        readMaterial,
        docSignals,
        isError: res?.isError ?? false,
        resultPreview: res?.preview ?? "",
      });
    }
  }

  return {
    meta,
    turns: boundaries,
    steps,
    heuristicChecks: scoreSide?.heuristicChecks,
    overallTranscriptPass: scoreSide?.overallTranscriptPass,
    llmOverallPass: scoreSide?.llmOverallPass,
  };
}

export function parseTranscriptRunJson(text: string): RunJson {
  return JSON.parse(text) as RunJson;
}
