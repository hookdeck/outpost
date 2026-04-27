/**
 * Generate a self-contained trajectory.html from transcript.json.
 *
 * Usage:
 *   npm run viz:trajectory -- --run results/runs/<stamp>-scenario-01
 *   npm run viz:trajectory -- --run results/runs/<stamp>-scenario-01/transcript.json --out /tmp/t.html
 */

import { readFile, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { parseArgs } from "node:util";
import {
  evalRootFromHere,
  labelStepsWithReference,
  tryLoadReferenceTrajectory,
  type PathLabel,
} from "./reference-trajectory.js";
import { resolveTranscriptJsonPath } from "./score-transcript.js";
import {
  extractRunTrajectory,
  parseTranscriptRunJson,
  type HeuristicCheckSummary,
  type TrajectoryPayload,
} from "./transcript-trajectory.js";

const EVAL_ROOT = evalRootFromHere(import.meta.url);

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

interface HeuristicScoreFile {
  transcript?: {
    checks?: Array<{ id: string; pass: boolean; detail: string }>;
  };
  overallTranscriptPass?: boolean | null;
}

interface LlmScoreFile {
  overall_transcript_pass?: boolean;
}

function embedJsonForScript(obj: unknown): string {
  return JSON.stringify(obj).replace(/</g, "\\u003c");
}

function buildHtml(payload: {
  trajectory: TrajectoryPayload;
  pathLabels: readonly PathLabel[] | null;
}): string {
  const dataJson = embedJsonForScript(payload);
  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Eval trajectory — scenario ${escapeHtml(String(payload.trajectory.meta.scenarioId ?? "?"))}</title>
  <style>
    :root {
      font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif;
      color: #0f172a;
      background: #f8fafc;
      line-height: 1.45;
    }
    body { margin: 0; padding: 1rem 1.25rem 2rem; }
    h1 { font-size: 1.15rem; margin: 0 0 0.5rem; }
    .meta { font-size: 0.85rem; color: #475569; margin-bottom: 1rem; word-break: break-all; }
    .strip { display: flex; flex-wrap: wrap; gap: 0.5rem; margin-bottom: 1rem; align-items: center; }
    .pill { font-size: 0.75rem; padding: 0.2rem 0.55rem; border-radius: 999px; background: #e2e8f0; }
    .pill.ok { background: #bbf7d0; color: #14532d; }
    .pill.fail { background: #fecaca; color: #7f1d1d; }
    .pill.na { background: #e2e8f0; color: #475569; }
    label.toggle { font-size: 0.8rem; user-select: none; }
    table { width: 100%; border-collapse: collapse; font-size: 0.8rem; background: #fff; box-shadow: 0 1px 2px rgb(15 23 42 / 0.08); border-radius: 8px; overflow: hidden; }
    th, td { text-align: left; padding: 0.45rem 0.5rem; border-bottom: 1px solid #e2e8f0; vertical-align: top; }
    th { background: #f1f5f9; font-weight: 600; position: sticky; top: 0; z-index: 1; }
    tr.step-row { cursor: pointer; }
    tr.step-row:hover { background: #f8fafc; }
    tr.step-row.selected { outline: 2px solid #2563eb; outline-offset: -2px; background: #eff6ff; }
    tr.step-row.on_path td:first-child { box-shadow: inset 3px 0 0 #22c55e; }
    tr.step-row.detour td:first-child { box-shadow: inset 3px 0 0 #f97316; }
    .tag { display: inline-block; font-size: 0.65rem; margin: 0.1rem 0.15rem 0 0; padding: 0.08rem 0.35rem; border-radius: 4px; background: #e0f2fe; color: #075985; }
    .tag.warn { background: #ffedd5; color: #9a3412; }
    .tag.err { background: #fee2e2; color: #991b1b; }
    .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 0.72rem; word-break: break-all; }
    .small { font-size: 0.72rem; color: #64748b; max-height: 3.2em; overflow: hidden; }
    button.clear { font-size: 0.75rem; padding: 0.25rem 0.5rem; }
    .filters { flex-direction: column; align-items: flex-start; gap: 0.35rem; max-width: 100%; }
    .filters .row { display: flex; flex-wrap: wrap; gap: 0.35rem 0.75rem; align-items: center; }
    .filters .hint { font-size: 0.72rem; color: #64748b; max-width: 52rem; }
    .filter-count { font-size: 0.8rem; color: #475569; margin: 0 0 0.75rem; }
    .tag.doc { background: #ecfccb; color: #365314; }
  </style>
</head>
<body>
  <h1>Agent eval trajectory</h1>
  <div class="meta" id="meta"></div>
  <div class="strip" id="scores"></div>
  <div class="strip">
    <label class="toggle"><input type="checkbox" id="hideExplore" /> Hide Glob / Grep / ToolSearch</label>
    <button type="button" class="clear" id="clearSel">Clear selection</button>
  </div>
  <p class="filter-count" id="filterCount"></p>
  <div class="strip filters" id="filterPanel">
    <div class="row">
      <strong style="font-size:0.8rem">Tool kind</strong>
      <label class="toggle"><input type="checkbox" class="fk" data-kind="read" checked /> read</label>
      <label class="toggle"><input type="checkbox" class="fk" data-kind="fetch" checked /> fetch</label>
      <label class="toggle"><input type="checkbox" class="fk" data-kind="bash" checked /> bash</label>
      <label class="toggle"><input type="checkbox" class="fk" data-kind="write" checked /> write</label>
      <label class="toggle"><input type="checkbox" class="fk" data-kind="search" checked /> search</label>
      <label class="toggle"><input type="checkbox" class="fk" data-kind="other" checked /> other</label>
    </div>
    <div class="row">
      <strong style="font-size:0.8rem">Read target</strong>
      <label class="toggle"><input type="checkbox" id="rmDoc" checked /> documentation (.md / .mdx / .mdoc / .rst)</label>
      <label class="toggle"><input type="checkbox" id="rmCode" checked /> code / other paths</label>
    </div>
    <div class="row">
      <strong style="font-size:0.8rem">Doc signals</strong>
      <span class="hint">When any box is checked, only <em>read</em> and <em>fetch</em> rows that match at least one signal are shown.</span>
    </div>
    <div class="row">
      <label class="toggle"><input type="checkbox" class="fd" data-sig="reference" /> reference</label>
      <label class="toggle"><input type="checkbox" class="fd" data-sig="openapi" /> openapi</label>
      <label class="toggle"><input type="checkbox" class="fd" data-sig="quickstart" /> quickstart</label>
      <label class="toggle"><input type="checkbox" class="fd" data-sig="published-docs" /> published docs (URL)</label>
      <label class="toggle"><input type="checkbox" class="fd" data-sig="concepts" /> concepts</label>
      <label class="toggle"><input type="checkbox" class="fd" data-sig="content-doc" /> content / .mdoc</label>
      <label class="toggle"><input type="checkbox" class="fd" data-sig="pages-doc" /> pages / .mdx</label>
      <label class="toggle"><input type="checkbox" class="fd" data-sig="sdk-source" /> SDK source</label>
      <label class="toggle"><input type="checkbox" class="fd" data-sig="destination-doc" /> destination doc</label>
      <label class="toggle"><input type="checkbox" class="fd" data-sig="prose-file" /> prose file (fallback)</label>
    </div>
  </div>
  <table>
    <thead>
      <tr>
        <th>#</th>
        <th>Turn</th>
        <th>Kind</th>
        <th>Material</th>
        <th>Tool</th>
        <th>Target / detail</th>
        <th>Tags</th>
        <th>Result</th>
      </tr>
    </thead>
    <tbody id="rows"></tbody>
  </table>
  <script>
    window.__TRAJECTORY__ = ${dataJson};
  </script>
  <script>
(function () {
  var data = window.__TRAJECTORY__;
  var traj = data.trajectory;
  var pathLabels = data.pathLabels;
  var metaEl = document.getElementById("meta");
  var scoresEl = document.getElementById("scores");
  var rowsEl = document.getElementById("rows");
  var hideExplore = document.getElementById("hideExplore");
  var filterCountEl = document.getElementById("filterCount");
  var filterPanel = document.getElementById("filterPanel");
  var rmDoc = document.getElementById("rmDoc");
  var rmCode = document.getElementById("rmCode");
  var clearSel = document.getElementById("clearSel");
  var selected = null;

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  var m = traj.meta || {};
  metaEl.innerHTML =
    "<strong>Scenario</strong> " + esc(m.scenarioId) + " — " + esc(m.scenarioFile) +
    "<br/><strong>Run</strong> " + esc(m.runDirectory) +
    "<br/><strong>Session</strong> " + esc(m.sessionId) + " &nbsp;|&nbsp; <strong>Model</strong> " + esc(m.model) +
    "<br/><strong>Completed</strong> " + esc(m.completedAt);

  if (traj.heuristicChecks && traj.heuristicChecks.length) {
    traj.heuristicChecks.forEach(function (c) {
      var span = document.createElement("span");
      span.className = "pill " + (c.pass ? "ok" : "fail");
      span.textContent = (c.pass ? "PASS " : "FAIL ") + c.id;
      span.title = c.detail;
      scoresEl.appendChild(span);
    });
  }
  var hp = traj.overallTranscriptPass;
  if (hp !== undefined && hp !== null) {
    var p = document.createElement("span");
    p.className = "pill " + (hp ? "ok" : "fail");
    p.textContent = "Heuristic overall: " + (hp ? "PASS" : "FAIL");
    scoresEl.appendChild(p);
  } else {
    var pna = document.createElement("span");
    pna.className = "pill na";
    pna.textContent = "No heuristic sidecar";
    scoresEl.appendChild(pna);
  }
  if (traj.llmOverallPass !== undefined && traj.llmOverallPass !== null) {
    var lp = document.createElement("span");
    lp.className = "pill " + (traj.llmOverallPass ? "ok" : "fail");
    lp.textContent = "LLM judge: " + (traj.llmOverallPass ? "PASS" : "FAIL");
    scoresEl.appendChild(lp);
  }

  function exploreTool(name) {
    return name === "Glob" || name === "Grep" || name === "ToolSearch";
  }

  function selectedDocSignals() {
    var out = [];
    filterPanel.querySelectorAll(".fd:checked").forEach(function (el) {
      var s = el.getAttribute("data-sig");
      if (s) out.push(s);
    });
    return out;
  }

  function materialLabel(step) {
    if (step.kind === "fetch") return "web";
    if (step.kind === "read") {
      if (step.readMaterial === "documentation") return "documentation";
      if (step.readMaterial === "code") return "code";
    }
    return "—";
  }

  function passesFilters(step) {
    if (hideExplore.checked && exploreTool(step.toolName)) return false;
    var fk = filterPanel.querySelector('.fk[data-kind="' + step.kind + '"]');
    if (fk && !fk.checked) return false;

    if (step.kind === "read") {
      var d = rmDoc.checked;
      var c = rmCode.checked;
      if (d || c) {
        if (d && !c && step.readMaterial !== "documentation") return false;
        if (!d && c && step.readMaterial !== "code") return false;
      }
    }

    var wantSigs = selectedDocSignals();
    if (wantSigs.length) {
      if (step.kind !== "read" && step.kind !== "fetch") return false;
      var ds = step.docSignals || [];
      var hit = wantSigs.some(function (s) {
        return ds.indexOf(s) >= 0;
      });
      if (!hit) return false;
    }
    return true;
  }

  function render() {
    rowsEl.innerHTML = "";
    var shown = 0;
    traj.steps.forEach(function (step, idx) {
      if (!passesFilters(step)) return;
      shown++;
      var tr = document.createElement("tr");
      tr.className = "step-row";
      tr.dataset.step = String(step.stepIndex);
      var pl = pathLabels && pathLabels[idx];
      if (pl === "on_path") tr.classList.add("on_path");
      if (pl === "detour") tr.classList.add("detour");
      if (step.isError) tr.classList.add("err-row");

      var td0 = document.createElement("td");
      td0.textContent = String(step.stepIndex);
      var td1 = document.createElement("td");
      td1.textContent = step.turnLabel;
      var td2 = document.createElement("td");
      td2.textContent = step.kind + (pl && pl !== "neutral" ? " (" + pl + ")" : "");
      var tdMat = document.createElement("td");
      tdMat.className = "small mono";
      tdMat.textContent = materialLabel(step);
      var td3 = document.createElement("td");
      td3.textContent = step.toolName;
      var td4 = document.createElement("td");
      td4.className = "mono";
      var primary = step.target || step.detail || "";
      td4.innerHTML = esc(primary);
      if (step.target && step.detail && step.kind === "fetch") {
        td4.innerHTML += "<div class=\\"small\\">" + esc(step.detail) + "</div>";
      }
      var td5 = document.createElement("td");
      (step.tags || []).forEach(function (t) {
        var sp = document.createElement("span");
        sp.className = "tag" + (/err|self-hosted|off-topic/i.test(t) ? " warn" : "");
        sp.textContent = t;
        td5.appendChild(sp);
      });
      (step.sdkHints || []).forEach(function (t) {
        var sp = document.createElement("span");
        sp.className = "tag";
        sp.textContent = t;
        td5.appendChild(sp);
      });
      (step.docSignals || []).forEach(function (t) {
        var sp = document.createElement("span");
        sp.className = "tag doc";
        sp.textContent = t;
        td5.appendChild(sp);
      });
      var td6 = document.createElement("td");
      td6.className = "small mono";
      td6.textContent = (step.isError ? "[error] " : "") + (step.resultPreview || "");

      tr.appendChild(td0);
      tr.appendChild(td1);
      tr.appendChild(td2);
      tr.appendChild(tdMat);
      tr.appendChild(td3);
      tr.appendChild(td4);
      tr.appendChild(td5);
      tr.appendChild(td6);

      tr.addEventListener("click", function () {
        if (selected) selected.classList.remove("selected");
        tr.classList.add("selected");
        selected = tr;
        location.hash = "s=" + step.stepIndex;
      });
      rowsEl.appendChild(tr);
    });
    filterCountEl.textContent =
      "Showing " + shown + " of " + traj.steps.length + " steps";
  }

  function applyHash() {
    var h = location.hash.replace(/^#/, "");
    var m2 = /^s=(\\d+)$/.exec(h);
    if (!m2) return;
    var n = m2[1];
    var tr = rowsEl.querySelector('tr[data-step="' + n + '"]');
    if (tr) {
      if (selected) selected.classList.remove("selected");
      tr.classList.add("selected");
      selected = tr;
      tr.scrollIntoView({ block: "center", behavior: "smooth" });
    }
  }

  hideExplore.addEventListener("change", render);
  filterPanel.addEventListener("change", render);
  clearSel.addEventListener("click", function () {
    if (selected) selected.classList.remove("selected");
    selected = null;
    history.replaceState(null, "", location.pathname + location.search);
  });
  window.addEventListener("hashchange", applyHash);
  render();
  applyHash();
})();
  </script>
</body>
</html>`;
}

async function tryReadHeuristicSidecar(
  transcriptPath: string,
): Promise<{
  checks: HeuristicCheckSummary[];
  overallTranscriptPass: boolean | null;
} | null> {
  const heuristicPath = join(dirname(transcriptPath), "heuristic-score.json");
  try {
    const raw = await readFile(heuristicPath, "utf8");
    const j = JSON.parse(raw) as HeuristicScoreFile;
    const checks = (j.transcript?.checks ?? []).map((c) => ({
      id: c.id,
      pass: c.pass,
      detail: c.detail,
    }));
    return {
      checks,
      overallTranscriptPass:
        j.overallTranscriptPass === undefined ? null : j.overallTranscriptPass,
    };
  } catch {
    return null;
  }
}

async function tryReadLlmOverall(transcriptPath: string): Promise<boolean | null> {
  const p = join(dirname(transcriptPath), "llm-score.json");
  try {
    const raw = await readFile(p, "utf8");
    const j = JSON.parse(raw) as LlmScoreFile;
    if (typeof j.overall_transcript_pass === "boolean") {
      return j.overall_transcript_pass;
    }
    return null;
  } catch {
    return null;
  }
}

export interface WriteTrajectoryHtmlOptions {
  /** Defaults to `docs/agent-evaluation/` resolved from this module. */
  readonly evalRoot?: string;
  /** Defaults to `<transcript-dir>/trajectory.html`. */
  readonly outPath?: string;
}

/**
 * Build and write `trajectory.html` for a finished run (used by CLI and `run-agent-eval`).
 */
export async function writeTrajectoryHtmlForTranscript(
  transcriptPath: string,
  options?: WriteTrajectoryHtmlOptions,
): Promise<string> {
  const evalRoot = options?.evalRoot ?? EVAL_ROOT;
  const rawText = await readFile(transcriptPath, "utf8");
  const run = parseTranscriptRunJson(rawText);

  const side = await tryReadHeuristicSidecar(transcriptPath);
  const llmPass = await tryReadLlmOverall(transcriptPath);

  const trajectory = extractRunTrajectory(run, {
    heuristicChecks: side?.checks,
    overallTranscriptPass: side?.overallTranscriptPass ?? null,
    llmOverallPass: llmPass,
  });

  const ref = await tryLoadReferenceTrajectory(
    trajectory.meta.scenarioId,
    evalRoot,
  );
  const pathLabels = ref ? labelStepsWithReference(trajectory.steps, ref) : null;

  const html = buildHtml({ trajectory, pathLabels });
  const out =
    options?.outPath?.trim() ||
    join(dirname(transcriptPath), "trajectory.html");
  await writeFile(out, html, "utf8");
  return out;
}

async function main(): Promise<void> {
  const { values } = parseArgs({
    options: {
      run: { type: "string" },
      out: { type: "string" },
      help: { type: "boolean", short: "h", default: false },
    },
    allowPositionals: false,
  });

  if (values.help || !values.run) {
    console.log(`
Generate trajectory.html from a transcript.json.

  npm run viz:trajectory -- --run results/runs/<stamp>-scenario-01
  npm run viz:trajectory -- --run results/runs/<stamp>-scenario-01/transcript.json
  npm run viz:trajectory -- --run <path> --out /tmp/out.html

Default output: <run-dir>/trajectory.html next to transcript.json.
`);
    process.exit(values.help ? 0 : 1);
  }

  const transcriptPath = await resolveTranscriptJsonPath(values.run);
  const outPath = await writeTrajectoryHtmlForTranscript(transcriptPath, {
    outPath: values.out?.trim() || undefined,
  });
  console.error(`Wrote ${outPath}`);
}

/** Only run CLI when this file is the process entrypoint (not when imported from run-agent-eval). */
const trajectoryCliEntry =
  typeof process.argv[1] === "string" &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href;

if (trajectoryCliEntry) {
  main().catch((e) => {
    console.error(e);
    process.exit(1);
  });
}
