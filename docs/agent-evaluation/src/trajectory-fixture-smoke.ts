/**
 * Smoke assertions for transcript-trajectory extraction (no test runner dependency).
 *
 *   npm run test:trajectory
 */

import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  buildTurnBoundaries,
  extractRunTrajectory,
  parseTranscriptRunJson,
} from "./transcript-trajectory.js";

const __dirname = dirname(fileURLToPath(import.meta.url));

function assert(cond: boolean, msg: string): void {
  if (!cond) {
    throw new Error(`Assertion failed: ${msg}`);
  }
}

async function main(): Promise<void> {
  const fixturePath = join(__dirname, "__fixtures__", "trajectory-minimal.json");
  const raw = await readFile(fixturePath, "utf8");
  const run = parseTranscriptRunJson(raw);

  const turns = buildTurnBoundaries(run.meta);
  assert(turns.length === 2, "expected two turn boundaries");
  assert(turns[0]!.label.includes("Turn 0"), "first turn label");
  assert(turns[0]!.startMessageIndex === 0 && turns[0]!.endMessageIndex === 4, "turn 0 message range");
  assert(turns[1]!.startMessageIndex === 5 && turns[1]!.endMessageIndex === 6, "turn 1 message range");

  const traj = extractRunTrajectory(run);
  assert(traj.steps.length === 3, `expected 3 tool steps, got ${traj.steps.length}`);
  assert(traj.steps[0]!.kind === "read", "first step read");
  assert(traj.steps[0]!.readMaterial === "documentation", "mdoc read is documentation");
  assert(
    traj.steps[0]!.docSignals.includes("quickstart") ||
      traj.steps[0]!.docSignals.includes("content-doc"),
    "curl quickstart path gets doc signal",
  );
  assert(
    traj.steps[0]!.target.includes("hookdeck-outpost-curl"),
    "read target contains curl quickstart",
  );
  assert(traj.steps[0]!.turnLabel.includes("Turn 0"), "first step turn label");
  assert(traj.steps[1]!.kind === "bash", "second step bash");
  assert(traj.steps[1]!.tags.includes("outpost-api-ish"), "bash tagged outpost-api-ish");
  assert(traj.steps[1]!.isError === true, "bash tool error");
  assert(traj.steps[2]!.kind === "write", "third step write");
  assert(traj.steps[2]!.target.endsWith("run.sh"), "write path");
  assert(traj.steps[2]!.turnLabel.includes("Turn 1"), "write step in user turn");

  console.error("trajectory-fixture-smoke: OK");
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
