/**
 * Heuristic transcript scoring for agent eval runs.
 * Maps to human checklist items in scenarios/*.md — not a substitute for execution verification.
 */

import { readFile, readdir, stat } from "node:fs/promises";
import { basename, dirname, join } from "node:path";

export interface CheckResult {
  readonly id: string;
  readonly pass: boolean;
  readonly detail: string;
}

export interface TranscriptScore {
  readonly passed: number;
  readonly total: number;
  readonly checks: readonly CheckResult[];
  readonly fraction: number;
}

export interface ScoreReport {
  readonly runFile: string;
  readonly scenarioId: string;
  readonly scenarioFile: string;
  readonly transcript: TranscriptScore;
  /** Automated harness does not run Outpost; execution stays manual or a future verifier. */
  readonly execution: { readonly status: "not_automated"; readonly note: string };
  /** null when no automated transcript rubric exists for this scenario yet */
  readonly overallTranscriptPass: boolean | null;
}

interface RunJson {
  meta?: {
    scenarioId?: string;
    scenarioFile?: string;
    turns?: readonly { label?: string; messageCount?: number }[];
  };
  messages?: unknown[];
}

export function extractAssistantText(messages: unknown[] | undefined): string {
  if (!messages?.length) return "";
  let out = "";
  for (const m of messages) {
    if (typeof m !== "object" || m === null) continue;
    const o = m as Record<string, unknown>;
    if (o.type !== "assistant") continue;
    const inner = o.message;
    if (typeof inner !== "object" || inner === null) continue;
    const msg = inner as Record<string, unknown>;
    const content = msg.content;
    if (!Array.isArray(content)) continue;
    for (const block of content) {
      if (typeof block !== "object" || block === null) continue;
      const b = block as Record<string, unknown>;
      if (b.type === "text" && typeof b.text === "string") {
        out += b.text;
      }
    }
  }
  return out;
}

const MAX_TOOL_SCORING_CHARS = 600_000;

/**
 * Assistant-visible text plus tool inputs and Write/Edit file bodies from the transcript.
 * Heuristics use this so scored content includes material that only appeared in tool calls/results.
 */
export function extractTranscriptScoringText(messages: unknown[] | undefined): string {
  const assistant = extractAssistantText(messages);
  if (!messages?.length) return assistant;
  const chunks: string[] = [];
  let budget = MAX_TOOL_SCORING_CHARS;

  const push = (s: string) => {
    if (budget <= 0) return;
    const take = s.slice(0, budget);
    chunks.push(take);
    budget -= take.length;
  };

  for (const m of messages) {
    if (typeof m !== "object" || m === null) continue;
    const o = m as Record<string, unknown>;

    if (o.type === "assistant") {
      const inner = o.message;
      if (typeof inner !== "object" || inner === null) continue;
      const content = (inner as Record<string, unknown>).content;
      if (!Array.isArray(content)) continue;
      for (const block of content) {
        if (typeof block !== "object" || block === null) continue;
        const b = block as Record<string, unknown>;
        if (b.type !== "tool_use") continue;
        const input = b.input;
        if (input !== undefined) {
          try {
            push(`\n[tool_use ${String(b.name ?? "?")}]\n${JSON.stringify(input)}\n`);
          } catch {
            push(`\n[tool_use ${String(b.name ?? "?")}]\n`);
          }
        }
      }
      continue;
    }

    if (o.type === "user") {
      const tur = o.tool_use_result;
      if (typeof tur === "object" && tur !== null) {
        const t = tur as Record<string, unknown>;
        if (typeof t.content === "string") {
          push(`\n[tool_result content]\n${t.content}\n`);
        }
        if (typeof t.newContent === "string") {
          push(`\n[tool_result newContent]\n${t.newContent}\n`);
        }
      }
      const inner = o.message;
      if (typeof inner === "object" && inner !== null) {
        const content = (inner as Record<string, unknown>).content;
        if (Array.isArray(content)) {
          for (const block of content) {
            if (typeof block !== "object" || block === null) continue;
            const b = block as Record<string, unknown>;
            if (b.type === "tool_result" && typeof b.content === "string") {
              push(`\n[tool_result]\n${b.content}\n`);
            }
          }
        }
      }
    }
  }

  return `${assistant}\n\n--- tool corpus ---\n${chunks.join("")}`;
}

function hadOptionalSecondUserTurn(meta: RunJson["meta"]): boolean {
  const turns = meta?.turns ?? [];
  return turns.some((t) => {
    const l = (t.label ?? "").toLowerCase();
    return l.includes("turn 2") || l.includes("optional");
  });
}

/** Likely pasted API key (not env var reference). */
function containsLikelyLeakedKey(text: string): boolean {
  if (/Bearer\s+sk-ant-api/i.test(text)) return true;
  if (/Bearer\s+[a-zA-Z0-9_-]{40,}/.test(text)) return true;
  return false;
}

/**
 * Option 3 (08–10): corpus should show publish on a real domain path, not only a synthetic
 * “test event” / publish-test helper. Multiple publish sites, or one publish without test-only
 * markers, passes. Weak signal — confirm with scenario Success criteria + execution smoke.
 */
function corpusSuggestsPublishBeyondTestOnly(corpus: string): boolean {
  const t = corpus;
  const publishHits = t.match(/publish\.event|Publish\.Event|PublishEvent/gi);
  if (!publishHits?.length) return false;
  if (publishHits.length >= 2) return true;
  const lower = t.toLowerCase();
  const testish =
    /publish-test|publish_test|publishtest|test_publish|send test|synthetic.*(event|publish)|test event/.test(
      lower,
    );
  if (!testish) return true;
  const domainish =
    /signup|register|user\.created|item\.|order\.|after_commit|post_save|on_.*create|createuser|create.?item|router\.(post|put|patch)|@router\.(post|put|patch)|handler\.|func.*create|def create_/.test(
      lower,
    ) && /publish|outpost/.test(lower);
  return domainish;
}

function scoreScenario01(corpus: string, assistant: string, meta: RunJson["meta"]): TranscriptScore {
  const t = corpus;
  const lower = t.toLowerCase();
  const checks: CheckResult[] = [];

  const managed =
    t.includes("api.outpost.hookdeck.com/2025-07-01") ||
    /\$OUTPOST_API_BASE_URL/.test(t);
  // Self-hosted snippet must not be what the assistant told the user to run (tool corpus can quote docs).
  const selfHostedInUserGuidance = /\blocalhost:3333\/api\/v1\b/.test(assistant);
  checks.push({
    id: "managed_base_url",
    pass: managed && !selfHostedInUserGuidance,
    detail: !managed
      ? "Expected api.outpost.hookdeck.com/2025-07-01 or $OUTPOST_API_BASE_URL"
      : selfHostedInUserGuidance
        ? "Assistant guidance includes localhost:3333/api/v1 (self-hosted) as primary"
        : "Uses managed API base (or OUTPOST_API_BASE_URL); no self-hosted path in assistant guidance",
  });

  const tenantPut =
    /PUT|put/i.test(t) &&
    (t.includes("/tenants/") || t.includes("/tenants/$") || t.includes("/tenants/${"));
  checks.push({
    id: "tenant_put",
    pass: tenantPut,
    detail: tenantPut ? "PUT …/tenants/… present" : "Expected PUT with /tenants/ path",
  });

  const dest =
    lower.includes("webhook") &&
    (t.includes("/destinations") || t.includes("/destinations\"")) &&
    (lower.includes("post") || t.includes("-X POST") || t.includes("-X post"));
  checks.push({
    id: "destination_webhook",
    pass: dest,
    detail: dest ? "POST destinations with webhook" : "Expected POST …/destinations with webhook type",
  });

  const publish =
    (t.includes("/publish") || t.includes("/publish\"")) &&
    (lower.includes("post") || t.includes("-X POST"));
  checks.push({
    id: "publish_post",
    pass: publish,
    detail: publish ? "POST …/publish present" : "Expected POST publish",
  });

  const afterPublish = t.split(/\/publish/i).pop() ?? t;
  // Tool corpus JSON-stringifies Write bodies, so bash-escaped keys look like \"data\": not "data":
  const wrongPayload =
    /"payload"\s*:/.test(afterPublish) || /\\"payload\\"\s*:/.test(afterPublish);
  const hasData =
    /"data"\s*:/.test(afterPublish) || /\\"data\\"\s*:/.test(afterPublish);
  checks.push({
    id: "publish_body_data_not_payload",
    pass: publish && !wrongPayload && hasData,
    detail: !publish
      ? "N/A (no publish block)"
      : wrongPayload
        ? 'Found "payload" after /publish — Outpost expects "data"'
        : hasData
          ? 'Publish section uses "data"'
          : 'Missing "data" in publish JSON (check manually)',
  });

  checks.push({
    id: "no_key_in_reply",
    pass: !containsLikelyLeakedKey(assistant),
    detail: containsLikelyLeakedKey(assistant)
      ? "Possible raw API key in assistant-visible text"
      : "No obvious raw Bearer secret in assistant text",
  });

  const verifyTurn = hadOptionalSecondUserTurn(meta);
  if (verifyTurn) {
    const verify =
      lower.includes("hookdeck") &&
      (lower.includes("console") || lower.includes("dashboard") || lower.includes("log"));
    checks.push({
      id: "verification_console_or_logs",
      pass: verify,
      detail: verify
        ? "Turn 2+ mentions Hookdeck Console / dashboard / logs"
        : "Optional verify turn ran but no Console/dashboard/logs mention found",
    });
  }

  const passed = checks.filter((c) => c.pass).length;
  const total = checks.length;
  return {
    passed,
    total,
    checks,
    fraction: total ? passed / total : 0,
  };
}

function scoreScenario02(corpus: string, assistant: string): TranscriptScore {
  const t = corpus;
  const checks: CheckResult[] = [];

  const sdk = /@hookdeck\/outpost-sdk\b/.test(t);
  checks.push({
    id: "ts_sdk_dependency",
    pass: sdk,
    detail: sdk ? "References @hookdeck/outpost-sdk" : "Expected @hookdeck/outpost-sdk in code or package.json",
  });

  const client = /new\s+Outpost\s*\(|Outpost\s*\(\s*\{/.test(t);
  checks.push({
    id: "outpost_client",
    pass: client,
    detail: client ? "Constructs Outpost client" : "Expected new Outpost(…) or Outpost({ … })",
  });

  const envKey = /process\.env\.OUTPOST_API_KEY|OUTPOST_API_KEY/.test(t);
  checks.push({
    id: "env_api_key",
    pass: envKey,
    detail: envKey ? "Uses OUTPOST_API_KEY from env" : "Expected process.env.OUTPOST_API_KEY (or documented env)",
  });

  const upsert = /tenants\.upsert|tenants\?\.upsert/.test(t);
  checks.push({
    id: "tenants_upsert",
    pass: upsert,
    detail: upsert ? "Calls tenants.upsert" : "Expected tenants.upsert",
  });

  const dest = /destinations\.create|destinations\?\.create/.test(t);
  checks.push({
    id: "destinations_create",
    pass: dest,
    detail: dest ? "Calls destinations.create" : "Expected destinations.create",
  });

  const pub = /publish\.event|publish\?\.event/.test(t);
  checks.push({
    id: "publish_event",
    pass: pub,
    detail: pub ? "Calls publish.event" : "Expected publish.event",
  });

  const hookUrl = /OUTPOST_TEST_WEBHOOK_URL/.test(t);
  checks.push({
    id: "webhook_env",
    pass: hookUrl,
    detail: hookUrl ? "Uses OUTPOST_TEST_WEBHOOK_URL" : "Expected OUTPOST_TEST_WEBHOOK_URL for webhook URL",
  });

  const run = /npx\s+tsx\b|tsx\s+\S+\.ts\b|ts-node\b|node\s+.*\.ts\b/.test(t);
  checks.push({
    id: "run_instructions",
    pass: run,
    detail: run ? "Mentions npx tsx / ts-node / running .ts" : "Expected run instructions (e.g. npx tsx …)",
  });

  checks.push({
    id: "no_key_in_reply",
    pass: !containsLikelyLeakedKey(assistant),
    detail: containsLikelyLeakedKey(assistant)
      ? "Possible raw API key in assistant-visible text"
      : "No obvious raw Bearer secret in assistant text",
  });

  const passed = checks.filter((c) => c.pass).length;
  const total = checks.length;
  return { passed, total, checks, fraction: total ? passed / total : 0 };
}

function scoreScenario03(corpus: string, assistant: string): TranscriptScore {
  const t = corpus;
  const checks: CheckResult[] = [];

  const imp = /from\s+outpost_sdk\s+import|import\s+outpost_sdk/.test(t);
  checks.push({
    id: "python_sdk_import",
    pass: imp,
    detail: imp ? "Imports outpost_sdk" : "Expected `from outpost_sdk import …` or import outpost_sdk",
  });

  const client = /Outpost\s*\(/.test(t);
  checks.push({
    id: "outpost_client",
    pass: client,
    detail: client ? "Constructs Outpost(…)" : "Expected Outpost(…) client",
  });

  const upsert = /tenants\.upsert|tenants\?\.upsert/.test(t);
  checks.push({
    id: "tenants_upsert",
    pass: upsert,
    detail: upsert ? "Calls tenants.upsert" : "Expected tenants.upsert",
  });

  const dest = /destinations\.create|destinations\?\.create/.test(t);
  checks.push({
    id: "destinations_create",
    pass: dest,
    detail: dest ? "Calls destinations.create" : "Expected destinations.create",
  });

  const pub = /publish\.event|publish\?\.event/.test(t);
  checks.push({
    id: "publish_event",
    pass: pub,
    detail: pub ? "Calls publish.event" : "Expected publish.event",
  });

  const env = /os\.environ|getenv\s*\(\s*["']OUTPOST_API_KEY/.test(t);
  checks.push({
    id: "env_api_key",
    pass: env,
    detail: env ? "Reads API key from environment" : "Expected os.environ or getenv for OUTPOST_API_KEY",
  });

  const hookUrl = /OUTPOST_TEST_WEBHOOK_URL/.test(t);
  checks.push({
    id: "webhook_env",
    pass: hookUrl,
    detail: hookUrl ? "Uses OUTPOST_TEST_WEBHOOK_URL" : "Expected OUTPOST_TEST_WEBHOOK_URL",
  });

  checks.push({
    id: "no_key_in_reply",
    pass: !containsLikelyLeakedKey(assistant),
    detail: containsLikelyLeakedKey(assistant)
      ? "Possible raw API key in assistant-visible text"
      : "No obvious raw Bearer secret in assistant text",
  });

  const passed = checks.filter((c) => c.pass).length;
  const total = checks.length;
  return { passed, total, checks, fraction: total ? passed / total : 0 };
}

function scoreScenario04(corpus: string, assistant: string): TranscriptScore {
  const t = corpus;
  const checks: CheckResult[] = [];

  const mod = /hookdeck\/outpost.*outpost-go|outpost-go|outpostgo/.test(t);
  checks.push({
    id: "go_sdk_module",
    pass: mod,
    detail: mod ? "References outpost-go / outpostgo" : "Expected github.com/hookdeck/outpost/.../outpost-go or outpostgo",
  });

  const newClient = /outpostgo\.New\s*\(|\bNew\s*\(\s*context\./.test(t);
  checks.push({
    id: "go_client_new",
    pass: newClient,
    detail: newClient ? "Creates client with New(…)" : "Expected outpostgo.New(…) or similar",
  });

  const sec = /WithSecurity|WithServerURL/.test(t);
  checks.push({
    id: "go_client_options",
    pass: sec,
    detail: sec ? "Uses WithSecurity or WithServerURL" : "Expected WithSecurity (and optional WithServerURL)",
  });

  const upsert = /Tenants\.Upsert|\.Upsert\s*\(/.test(t);
  checks.push({
    id: "tenants_upsert",
    pass: upsert,
    detail: upsert ? "Calls Tenants.Upsert" : "Expected Tenants.Upsert",
  });

  const dest = /Destinations\.Create|CreateDestinationCreateWebhook/.test(t);
  checks.push({
    id: "destinations_create",
    pass: dest,
    detail: dest ? "Creates webhook destination" : "Expected Destinations.Create / CreateDestinationCreateWebhook",
  });

  const pub = /Publish\.Event|\.Event\s*\(/.test(t);
  checks.push({
    id: "publish_event",
    pass: pub,
    detail: pub ? "Calls Publish.Event" : "Expected Publish.Event",
  });

  const envKey = /Getenv\s*\(\s*["']OUTPOST_API_KEY["']/.test(t);
  checks.push({
    id: "env_api_key",
    pass: envKey,
    detail: envKey ? "Reads OUTPOST_API_KEY via os.Getenv" : "Expected os.Getenv(\"OUTPOST_API_KEY\")",
  });

  const hookUrl = /OUTPOST_TEST_WEBHOOK_URL/.test(t);
  checks.push({
    id: "webhook_env",
    pass: hookUrl,
    detail: hookUrl ? "Uses OUTPOST_TEST_WEBHOOK_URL" : "Expected OUTPOST_TEST_WEBHOOK_URL",
  });

  checks.push({
    id: "no_key_in_reply",
    pass: !containsLikelyLeakedKey(assistant),
    detail: containsLikelyLeakedKey(assistant)
      ? "Possible raw API key in assistant-visible text"
      : "No obvious raw Bearer secret in assistant text",
  });

  const passed = checks.filter((c) => c.pass).length;
  const total = checks.length;
  return { passed, total, checks, fraction: total ? passed / total : 0 };
}

function scoreScenario05(corpus: string, assistant: string, meta: RunJson["meta"]): TranscriptScore {
  const t = corpus;
  const lower = t.toLowerCase();
  const checks: CheckResult[] = [];

  const next =
    /"next"\s*:\s*"/.test(t) ||
    /next\/dev|next\s+dev|next\.config/.test(t) ||
    /\bnext@\d/.test(t);
  checks.push({
    id: "nextjs_signals",
    pass: next,
    detail: next ? "Next.js dependency or dev command present" : "Expected next in package.json or next dev / next.config",
  });

  const sdk = /@hookdeck\/outpost-sdk\b/.test(t);
  checks.push({
    id: "outpost_ts_sdk",
    pass: sdk,
    detail: sdk ? "Uses @hookdeck/outpost-sdk" : "Expected @hookdeck/outpost-sdk in dependencies or imports",
  });

  const api =
    /app\/api\/[^"'\s]+\/route\.(t|j)sx?/.test(t) ||
    /pages\/api\//.test(t) ||
    /["']\/api\/(destination|destinations|event|publish)/.test(t);
  checks.push({
    id: "api_routes_layer",
    pass: api,
    detail: api ? "App/Pages API route layer present" : "Expected app/api/.../route or pages/api or /api/… fetches",
  });

  const twoFlows =
    (/destination|webhook|subscribe/i.test(t) && /publish|event|send/i.test(t) && /\/api\//.test(t)) ||
    (t.includes("/api/destination") && t.includes("/api/event"));
  checks.push({
    id: "destination_and_publish_surface",
    pass: twoFlows,
    detail: twoFlows
      ? "Distinct destination + publish flows (URLs or labels)"
      : "Expected separate destination registration and publish (e.g. two API routes or actions)",
  });

  const serverEnv =
    /route\.(t|j)sx?[\s\S]{0,12000}process\.env\.OUTPOST_API_KEY|OUTPOST_API_KEY[\s\S]{0,800}(route\.(t|j)sx?|api\/)/i.test(
      t,
    ) || (/process\.env\.OUTPOST_API_KEY/.test(t) && /app\/api\//.test(t));
  checks.push({
    id: "server_env_outpost_key",
    pass: serverEnv,
    detail: serverEnv
      ? "OUTPOST_API_KEY read server-side (e.g. API route)"
      : "Expected process.env.OUTPOST_API_KEY in API route context",
  });

  const leakClient = /NEXT_PUBLIC_OUTPOST_API_KEY/.test(t);
  checks.push({
    id: "no_next_public_api_key",
    pass: !leakClient,
    detail: leakClient
      ? "NEXT_PUBLIC_OUTPOST_API_KEY would expose key to browser"
      : "No NEXT_PUBLIC_OUTPOST_API_KEY",
  });

  const readme = /README/i.test(t) && /OUTPOST_API_KEY/.test(t);
  checks.push({
    id: "readme_env",
    pass: readme,
    detail: readme ? "README mentions OUTPOST_API_KEY" : "Expected README with OUTPOST_API_KEY",
  });

  const managed =
    !/\blocalhost:3333\/api\/v1\b/.test(t) &&
    (!/localhost:\d{2,5}\s*\/\s*api\/v1/.test(t) || /OUTPOST_API_BASE_URL/.test(t));
  checks.push({
    id: "managed_base_not_selfhosted",
    pass: managed,
    detail: managed
      ? "No self-hosted localhost API path as default"
      : "Avoid localhost:3333/api/v1 unless user asked for self-hosted",
  });

  checks.push({
    id: "no_key_in_reply",
    pass: !containsLikelyLeakedKey(assistant),
    detail: containsLikelyLeakedKey(assistant)
      ? "Possible raw API key in assistant-visible text"
      : "No obvious raw Bearer secret in assistant text",
  });

  const stressTurn = (meta?.turns?.length ?? 0) >= 4;
  if (stressTurn) {
    const hookdeckHint =
      lower.includes("hookdeck") &&
      (lower.includes("console") || lower.includes("source") || lower.includes("dashboard"));
    checks.push({
      id: "stress_public_url_hint",
      pass: hookdeckHint,
      detail: hookdeckHint
        ? "Turn 3+ stress: mentions Hookdeck Console/Source/dashboard for webhook URL"
        : "Stress turn present but no Hookdeck Console/Source hint found",
    });
  }

  const passed = checks.filter((c) => c.pass).length;
  const total = checks.length;
  return { passed, total, checks, fraction: total ? passed / total : 0 };
}

function scoreScenario06(corpus: string, assistant: string): TranscriptScore {
  const t = corpus;
  const lower = t.toLowerCase();
  const checks: CheckResult[] = [];

  const fast = /FastAPI|from\s+fastapi\s+import/.test(t);
  checks.push({
    id: "fastapi_framework",
    pass: fast,
    detail: fast ? "Uses FastAPI" : "Expected FastAPI import or class",
  });

  const sdk = /from\s+outpost_sdk\s+import|import\s+outpost_sdk|outpost_sdk/.test(t);
  checks.push({
    id: "python_outpost_sdk",
    pass: sdk,
    detail: sdk ? "Uses outpost_sdk" : "Expected outpost_sdk import or usage",
  });

  const uv = /uvicorn/.test(lower);
  checks.push({
    id: "uvicorn_documented",
    pass: uv,
    detail: uv ? "Mentions uvicorn" : "Expected uvicorn run command or import",
  });

  const envKey = /OUTPOST_API_KEY/.test(t) && (/os\.environ|getenv/.test(t) || /Depends?\(/.test(t));
  checks.push({
    id: "server_env_api_key",
    pass: envKey,
    detail: envKey ? "API key from environment on server" : "Expected OUTPOST_API_KEY via os.environ/getenv or settings",
  });

  const two =
    (/destination|webhook/i.test(t) && /publish|event/i.test(t)) ||
    (/@app\.(get|post)|APIRouter/.test(t) && /publish/i.test(t) && /destination|webhook/i.test(t));
  checks.push({
    id: "register_and_publish_flow",
    pass: two,
    detail: two ? "Both destination/webhook and publish/event surfaced" : "Expected register webhook + publish flows",
  });

  const readme = /README/i.test(t) && /OUTPOST_API_KEY/.test(t);
  checks.push({
    id: "readme_env",
    pass: readme,
    detail: readme ? "README mentions OUTPOST_API_KEY" : "Expected README with OUTPOST_API_KEY",
  });

  const hookOrDoc = /OUTPOST_TEST_WEBHOOK_URL|TEST_WEBHOOK|webhook\s*url/i.test(t);
  checks.push({
    id: "webhook_url_documented",
    pass: hookOrDoc,
    detail: hookOrDoc ? "Webhook URL env or field documented" : "Expected OUTPOST_TEST_WEBHOOK_URL or webhook URL docs",
  });

  checks.push({
    id: "no_key_in_reply",
    pass: !containsLikelyLeakedKey(assistant),
    detail: containsLikelyLeakedKey(assistant)
      ? "Possible raw API key in assistant-visible text"
      : "No obvious raw Bearer secret in assistant text",
  });

  const passed = checks.filter((c) => c.pass).length;
  const total = checks.length;
  return { passed, total, checks, fraction: total ? passed / total : 0 };
}

function scoreScenario07(corpus: string, assistant: string): TranscriptScore {
  const t = corpus;
  const lower = t.toLowerCase();
  const checks: CheckResult[] = [];

  const httpLib = /"net\/http"|net\/http/.test(t) || /\bhttp\.HandleFunc\b/.test(t);
  checks.push({
    id: "stdlib_http",
    pass: httpLib,
    detail: httpLib ? "Uses net/http" : "Expected net/http or http.HandleFunc",
  });

  const sdk = /hookdeck\/outpost.*outpost-go|outpostgo|CreateDestinationCreateWebhook/.test(t);
  checks.push({
    id: "go_outpost_sdk",
    pass: sdk,
    detail: sdk ? "Uses Outpost Go SDK patterns" : "Expected outpost-go / CreateDestinationCreateWebhook",
  });

  const createWebhook = /CreateDestinationCreateWebhook/.test(t);
  checks.push({
    id: "create_destination_webhook",
    pass: createWebhook,
    detail: createWebhook ? "CreateDestinationCreateWebhook present" : "Expected CreateDestinationCreateWebhook wrapper",
  });

  const htmlUi = /<form|<button|text\/html|template\.Execute/.test(t);
  checks.push({
    id: "html_ui",
    pass: htmlUi,
    detail: htmlUi ? "HTML form/button or template response" : "Expected simple HTML UI",
  });

  const two =
    (/destination|webhook/i.test(t) && /publish|event/i.test(t)) ||
    (/register|destination/i.test(lower) && /publish/i.test(lower));
  checks.push({
    id: "destination_and_publish_ui",
    pass: two,
    detail: two ? "Destination + publish reflected in UI or handlers" : "Expected both create destination and publish flows",
  });

  const envKey = /Getenv\s*\(\s*["']OUTPOST_API_KEY["']/.test(t);
  checks.push({
    id: "env_api_key",
    pass: envKey,
    detail: envKey ? "Reads OUTPOST_API_KEY from env" : "Expected os.Getenv(\"OUTPOST_API_KEY\")",
  });

  const run = /go\s+run\b/.test(lower);
  checks.push({
    id: "go_run_documented",
    pass: run,
    detail: run ? "Documents go run" : "Expected `go run` instructions",
  });

  const readme = /README/i.test(t) && (/OUTPOST_API_KEY|port/i.test(t));
  checks.push({
    id: "readme_env_or_port",
    pass: readme,
    detail: readme ? "README mentions env or port" : "Expected README with OUTPOST_API_KEY or port",
  });

  checks.push({
    id: "no_key_in_reply",
    pass: !containsLikelyLeakedKey(assistant),
    detail: containsLikelyLeakedKey(assistant)
      ? "Possible raw API key in assistant-visible text"
      : "No obvious raw Bearer secret in assistant text",
  });

  const passed = checks.filter((c) => c.pass).length;
  const total = checks.length;
  return { passed, total, checks, fraction: total ? passed / total : 0 };
}

/** Option 3 — integrate Outpost into an existing SaaS-style codebase (Next.js baseline). */
function scoreScenario08(corpus: string, assistant: string): TranscriptScore {
  const t = corpus;
  const lower = t.toLowerCase();
  const checks: CheckResult[] = [];

  const baseline =
    /leerob\/next-saas-starter|next-saas-starter/.test(t) ||
    (/git\s+clone\b/.test(lower) && /github\.com/.test(t));
  checks.push({
    id: "baseline_or_clone",
    pass: baseline,
    detail: baseline
      ? "References next-saas-starter baseline or git clone from GitHub"
      : "Expected clone/setup of the documented baseline (e.g. leerob/next-saas-starter)",
  });

  const sdk = /@hookdeck\/outpost-sdk\b/.test(t);
  checks.push({
    id: "outpost_ts_sdk",
    pass: sdk,
    detail: sdk ? "Uses @hookdeck/outpost-sdk" : "Expected @hookdeck/outpost-sdk",
  });

  const integration =
    /publish\.event|destinations\.create|tenants\.upsert/.test(t) ||
    /\/api\/.*outpost|outpost.*publish/i.test(t);
  checks.push({
    id: "outpost_integration_calls",
    pass: integration,
    detail: integration
      ? "Server-side Outpost client usage (publish / destinations / tenants)"
      : "Expected publish.event, destinations.create, or tenants.upsert (or clear API wrapper)",
  });

  const topic = /user\.created|topic|TOPIC/.test(t);
  checks.push({
    id: "topic_or_event_hook",
    pass: topic,
    detail: topic ? "Topic or event hook documented" : "Expected topic from prompt or explicit event naming",
  });

  const serverKey =
    /process\.env\.OUTPOST_API_KEY/.test(t) &&
    !/NEXT_PUBLIC_OUTPOST_API_KEY/.test(t);
  checks.push({
    id: "server_env_key_only",
    pass: serverKey,
    detail: serverKey
      ? "OUTPOST_API_KEY read server-side; no NEXT_PUBLIC_ key"
      : "Expected process.env.OUTPOST_API_KEY and no NEXT_PUBLIC_OUTPOST_API_KEY",
  });

  const destDoc =
    /destination|webhook\s*url|register.*webhook/i.test(t) && /tenant|customer|team/i.test(lower);
  checks.push({
    id: "destination_per_customer_doc",
    pass: destDoc,
    detail: destDoc
      ? "Documents webhook destination registration per tenant/customer (or team)"
      : "Expected how operators register webhook URLs per customer/tenant",
  });

  const beyondTest = corpusSuggestsPublishBeyondTestOnly(t);
  checks.push({
    id: "publish_beyond_test_only",
    pass: beyondTest,
    detail: beyondTest
      ? "Publish appears beyond a synthetic test-only path (or multiple publish sites)"
      : "Expected domain publish (not only publish-test / send test) — see scenario Success criteria",
  });

  const fullStackSignals =
    /(attempt|retry|list\s*attempt|destination[_-]?scoped|\/activity|\/attempts|events?\s*\(|list\s*events|manual\s*retry)/i.test(
      t,
    ) && /(outpost|destination|tenant)/i.test(t);
  checks.push({
    id: "delivery_activity_signals",
    pass: fullStackSignals,
    detail: fullStackSignals
      ? "Transcript mentions delivery visibility (attempts/events/retry/activity) with Outpost context"
      : "Scenario 8 expects destination-scoped activity UI — see Building your own UI checklists + success criteria",
  });

  const testPublishSeparate =
    /(test\s*publish|publish\s*test|send\s*test\s*event|\/api\/.*test|test.?event)/i.test(t);
  checks.push({
    id: "separate_test_publish_signal",
    pass: testPublishSeparate,
    detail: testPublishSeparate
      ? "Separate test publish / test event control mentioned"
      : "Expected distinct test-publish path or control (see scenario 8 success criteria)",
  });

  checks.push({
    id: "no_key_in_reply",
    pass: !containsLikelyLeakedKey(assistant),
    detail: containsLikelyLeakedKey(assistant)
      ? "Possible raw API key in assistant-visible text"
      : "No obvious raw Bearer secret in assistant text",
  });

  const passed = checks.filter((c) => c.pass).length;
  const total = checks.length;
  return { passed, total, checks, fraction: total ? passed / total : 0 };
}

/** Option 3 — existing FastAPI SaaS baseline. */
function scoreScenario09(corpus: string, assistant: string): TranscriptScore {
  const t = corpus;
  const lower = t.toLowerCase();
  const checks: CheckResult[] = [];

  const baseline =
    /philipokiokio\/fastapi_saas_template|fastapi_saas_template|FastAPI_SAAS/i.test(t) ||
    /fastapi\/full-stack-fastapi-template|full-stack-fastapi-template|full_stack_fastapi_template/i.test(
      t,
    ) ||
    (/git\s+clone\b/.test(lower) && /github\.com/.test(t));
  checks.push({
    id: "baseline_or_clone",
    pass: baseline,
    detail: baseline
      ? "References FastAPI baseline (full-stack template or legacy SaaS template) or git clone"
      : "Expected clone/setup of fastapi/full-stack-fastapi-template (or documented alternative)",
  });

  const sdk = /from\s+outpost_sdk\s+import|import\s+outpost_sdk/.test(t);
  checks.push({
    id: "python_outpost_sdk",
    pass: sdk,
    detail: sdk ? "Imports outpost_sdk" : "Expected outpost_sdk import",
  });

  const integration =
    /publish\.event|destinations\.create|tenants\.upsert/.test(t);
  checks.push({
    id: "outpost_integration_calls",
    pass: integration,
    detail: integration ? "Uses tenants/destinations/publish APIs" : "Expected SDK API calls for Outpost",
  });

  const hook =
    /signal|event|webhook|post_save|after_create|lifecycle|router\.(post|put)/i.test(t) &&
    /publish|outpost/i.test(lower);
  checks.push({
    id: "domain_event_hook",
    pass: hook,
    detail: hook
      ? "Hooks Outpost publish into an application event or route"
      : "Expected tying publish to a domain event or HTTP handler",
  });

  const env = /OUTPOST_API_KEY/.test(t) && (/os\.environ|getenv|settings|Depends/.test(t));
  checks.push({
    id: "env_api_key",
    pass: env,
    detail: env ? "API key from environment / settings" : "Expected OUTPOST_API_KEY from env",
  });

  const clientKeyLeak =
    /NEXT_PUBLIC_OUTPOST_API_KEY\s*[=:]/.test(t) ||
    /VITE_OUTPOST_API_KEY\s*[=:]/.test(t) ||
    /process\.env\.NEXT_PUBLIC_OUTPOST_API_KEY\b/.test(t) ||
    /import\.meta\.env\.(?:VITE_OUTPOST_API_KEY|NEXT_PUBLIC_OUTPOST_API_KEY)\b/.test(t);
  checks.push({
    id: "no_client_bundled_outpost_key",
    pass: !clientKeyLeak,
    detail: clientKeyLeak
      ? "Corpus suggests Outpost API key wired into client-visible env — keep server-side only"
      : "No client env assignment/access for OUTPOST_API_KEY (NEXT_PUBLIC_/VITE_) in corpus",
  });

  const beyondTest = corpusSuggestsPublishBeyondTestOnly(t);
  checks.push({
    id: "publish_beyond_test_only",
    pass: beyondTest,
    detail: beyondTest
      ? "Publish appears beyond a synthetic test-only path (or multiple publish sites)"
      : "Expected domain publish (not only publish-test / send test) — see scenario Success criteria",
  });

  const readmeOrEnvDocs =
    /OUTPOST_API_KEY/.test(t) &&
    /README|development\.md|\.env\.example|backend\/readme/i.test(t);
  checks.push({
    id: "readme_or_env_docs",
    pass: readmeOrEnvDocs,
    detail: readmeOrEnvDocs
      ? "README / development.md / .env.example (or similar) touches OUTPOST_API_KEY"
      : "Expected operator docs listing OUTPOST env vars (see scenario Success criteria)",
  });

  const fullStackSignals09 =
    /(attempt|retry|list\s*attempt|destination[_-]?scoped|\/activity|\/attempts|events?\s*\(|list\s*events|manual\s*retry)/i.test(
      t,
    ) && /(outpost|destination|tenant)/i.test(t);
  checks.push({
    id: "delivery_activity_signals",
    pass: fullStackSignals09,
    detail: fullStackSignals09
      ? "Transcript mentions delivery visibility (attempts/events/retry/activity) with Outpost context"
      : "Scenario 9 expects full-stack activity UI — see Building your own UI checklists + success criteria",
  });

  checks.push({
    id: "no_key_in_reply",
    pass: !containsLikelyLeakedKey(assistant),
    detail: containsLikelyLeakedKey(assistant)
      ? "Possible raw API key in assistant-visible text"
      : "No obvious raw Bearer secret in assistant text",
  });

  const passed = checks.filter((c) => c.pass).length;
  const total = checks.length;
  return { passed, total, checks, fraction: total ? passed / total : 0 };
}

/** Option 3 — existing Go SaaS/API baseline. */
function scoreScenario10(corpus: string, assistant: string): TranscriptScore {
  const t = corpus;
  const lower = t.toLowerCase();
  const checks: CheckResult[] = [];

  const baseline =
    /devinterface\/startersaas-go-api|startersaas-go-api|StarterSaaS/.test(t) ||
    (/git\s+clone\b/.test(lower) && /github\.com/.test(t));
  checks.push({
    id: "baseline_or_clone",
    pass: baseline,
    detail: baseline
      ? "References StarterSaaS Go API baseline or git clone"
      : "Expected clone/setup of devinterface/startersaas-go-api (or documented alternative)",
  });

  const sdk = /hookdeck\/outpost.*outpost-go|outpostgo\.|github\.com\/hookdeck\/outpost/.test(t);
  checks.push({
    id: "go_outpost_sdk",
    pass: sdk,
    detail: sdk ? "Uses Outpost Go module" : "Expected outpost-go / outpostgo import path",
  });

  const integration = /Publish\.Event|Tenants\.|Destinations\./.test(t);
  checks.push({
    id: "outpost_integration_calls",
    pass: integration,
    detail: integration ? "Uses Outpost Go client operations" : "Expected Publish / Tenants / Destinations usage",
  });

  const hook =
    /handler|middleware|OnUser|event|CreateUser|signup|register/i.test(t) && /publish|outpost/i.test(lower);
  checks.push({
    id: "domain_event_hook",
    pass: hook,
    detail: hook
      ? "Hooks publish into a handler or domain flow"
      : "Expected publish tied to a concrete code path",
  });

  const envKey = /Getenv\s*\(\s*["']OUTPOST_API_KEY["']/.test(t);
  checks.push({
    id: "env_api_key",
    pass: envKey,
    detail: envKey ? "Reads OUTPOST_API_KEY via os.Getenv" : "Expected os.Getenv(\"OUTPOST_API_KEY\")",
  });

  const beyondTest = corpusSuggestsPublishBeyondTestOnly(t);
  checks.push({
    id: "publish_beyond_test_only",
    pass: beyondTest,
    detail: beyondTest
      ? "Publish appears beyond a synthetic test-only path (or multiple publish sites)"
      : "Expected domain publish (not only publish-test / send test) — see scenario Success criteria",
  });

  checks.push({
    id: "no_key_in_reply",
    pass: !containsLikelyLeakedKey(assistant),
    detail: containsLikelyLeakedKey(assistant)
      ? "Possible raw API key in assistant-visible text"
      : "No obvious raw Bearer secret in assistant text",
  });

  const passed = checks.filter((c) => c.pass).length;
  const total = checks.length;
  return { passed, total, checks, fraction: total ? passed / total : 0 };
}

/** Scenarios with a non-empty regex rubric in this file (used for exit / overallTranscriptPass). */
export const SCENARIO_IDS_WITH_HEURISTIC_RUBRIC: ReadonlySet<string> = new Set([
  "01",
  "02",
  "03",
  "04",
  "05",
  "06",
  "07",
  "08",
  "09",
  "10",
]);

function scoreByScenarioId(
  scenarioId: string,
  corpus: string,
  assistant: string,
  meta: RunJson["meta"],
): TranscriptScore {
  switch (scenarioId) {
    case "01":
      return scoreScenario01(corpus, assistant, meta);
    case "02":
      return scoreScenario02(corpus, assistant);
    case "03":
      return scoreScenario03(corpus, assistant);
    case "04":
      return scoreScenario04(corpus, assistant);
    case "05":
      return scoreScenario05(corpus, assistant, meta);
    case "06":
      return scoreScenario06(corpus, assistant);
    case "07":
      return scoreScenario07(corpus, assistant);
    case "08":
      return scoreScenario08(corpus, assistant);
    case "09":
      return scoreScenario09(corpus, assistant);
    case "10":
      return scoreScenario10(corpus, assistant);
    default:
      return {
        passed: 0,
        total: 0,
        checks: [],
        fraction: 0,
      };
  }
}

export async function scoreRunJson(
  runPath: string,
  raw: string,
): Promise<ScoreReport> {
  const data = JSON.parse(raw) as RunJson;
  const scenarioId = data.meta?.scenarioId ?? "unknown";
  const scenarioFile = data.meta?.scenarioFile ?? `${scenarioId}-unknown.md`;
  const assistantOnly = extractAssistantText(data.messages);
  const corpus = extractTranscriptScoringText(data.messages);
  const transcript = scoreByScenarioId(scenarioId, corpus, assistantOnly, data.meta);

  const hasRubric = SCENARIO_IDS_WITH_HEURISTIC_RUBRIC.has(scenarioId);
  const overallTranscriptPass = hasRubric
    ? transcript.total > 0 && transcript.passed === transcript.total
    : null;

  return {
    runFile: runPath,
    scenarioId,
    scenarioFile,
    transcript,
    execution: {
      status: "not_automated",
      note:
        "Execution (live Outpost) is not scored here. After running curls/code with OUTPOST_API_KEY, mark the Execution row in scenarios/*.md or results/RUN-RECORDING.template.md.",
    },
    overallTranscriptPass,
  };
}

export async function scoreRunFile(runPath: string): Promise<ScoreReport> {
  const raw = await readFile(runPath, "utf8");
  return scoreRunJson(runPath, raw);
}

/** Resolve a run directory or legacy flat JSON path to transcript.json path. */
export async function resolveTranscriptJsonPath(input: string): Promise<string> {
  let st;
  try {
    st = await stat(input);
  } catch {
    throw new Error(`Path not found: ${input}`);
  }
  if (st.isDirectory()) {
    const t = join(input, "transcript.json");
    try {
      await stat(t);
    } catch {
      throw new Error(`No transcript.json in directory: ${input}`);
    }
    return t;
  }
  return input;
}

/** Sidecar score paths: nested run dir vs legacy flat *-scenario-NN.json */
export function scoreSidecarPaths(transcriptPath: string): {
  heuristic: string;
  llm: string;
} {
  if (basename(transcriptPath) === "transcript.json") {
    const dir = dirname(transcriptPath);
    return {
      heuristic: join(dir, "heuristic-score.json"),
      llm: join(dir, "llm-score.json"),
    };
  }
  return {
    heuristic: transcriptPath.replace(/\.json$/i, ".score.json"),
    llm: transcriptPath.replace(/\.json$/i, ".llm-score.json"),
  };
}

export async function findLatestRunFile(
  runsDir: string,
  scenarioId?: string,
): Promise<string | null> {
  const entries = await readdir(runsDir, { withFileTypes: true });
  /** Mutable holder so TS control flow tracks updates across async `consider` calls. */
  const latest = { path: null as string | null, mtime: -Infinity };

  const consider = async (transcriptPath: string) => {
    try {
      const st = await stat(transcriptPath);
      if (st.mtimeMs > latest.mtime) {
        latest.path = transcriptPath;
        latest.mtime = st.mtimeMs;
      }
    } catch {
      /* skip */
    }
  };

  for (const ent of entries) {
    const name = ent.name;
    if (ent.isDirectory()) {
      if (!/-scenario-\d{2}$/i.test(name)) continue;
      if (
        scenarioId &&
        !name.endsWith(`scenario-${scenarioId.padStart(2, "0")}`)
      ) {
        continue;
      }
      await consider(join(runsDir, name, "transcript.json"));
      continue;
    }
    if (
      ent.isFile() &&
      /-scenario-\d{2}\.json$/i.test(name) &&
      !name.endsWith(".score.json") &&
      !name.endsWith(".llm-score.json")
    ) {
      if (
        scenarioId &&
        !name.includes(`scenario-${scenarioId.padStart(2, "0")}`)
      ) {
        continue;
      }
      await consider(join(runsDir, name));
    }
  }

  return latest.path;
}

export function formatScoreReportHuman(r: ScoreReport): string {
  const lines: string[] = [
    `Transcript: ${r.runFile}`,
    `Scenario: ${r.scenarioId} (${r.scenarioFile})`,
  ];
  if (basename(r.runFile) === "transcript.json") {
    lines.push(`Run directory (agent workspace): ${dirname(r.runFile)}`);
  }
  lines.push("");
  if (r.transcript.total === 0) {
    lines.push("Transcript checks: (no automated rubric — add scorers in src/score-transcript.ts)");
  } else {
    lines.push(
      `Transcript checks: ${r.transcript.passed}/${r.transcript.total} passed (${Math.round(r.transcript.fraction * 100)}%)`,
    );
  }
  for (const c of r.transcript.checks) {
    lines.push(`  [${c.pass ? "PASS" : "FAIL"}] ${c.id}: ${c.detail}`);
  }
  lines.push("");
  lines.push(`Execution: ${r.execution.status} — ${r.execution.note}`);
  lines.push("");
  lines.push(
    `Overall transcript pass: ${
      r.overallTranscriptPass === null ? "N/A (no rubric)" : r.overallTranscriptPass ? "YES" : "NO"
    }`,
  );
  return lines.join("\n");
}
