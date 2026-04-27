/**
 * Optional reference milestones for "green path" vs detour labeling (scenario-specific JSON).
 */

import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { TrajectoryKind, TrajectoryStep } from "./transcript-trajectory.js";

export interface MilestoneMatch {
  readonly kind?: TrajectoryKind;
  /** Substring match on step.target */
  readonly targetContains?: string;
  readonly targetEndsWith?: string;
  readonly toolName?: string;
  readonly detailContains?: string;
}

export interface Milestone {
  readonly id: string;
  /** Satisfy any one of these */
  readonly anyOf?: readonly MilestoneMatch[];
  /** Single match (same as one anyOf entry) */
  readonly match?: MilestoneMatch;
}

export interface ReferenceTrajectoryFile {
  readonly scenarioId: string;
  readonly milestones: readonly Milestone[];
}

function singleMatch(m: Milestone): MilestoneMatch[] {
  if (m.anyOf?.length) return [...m.anyOf];
  if (m.match) return [m.match];
  return [];
}

function matchOne(step: TrajectoryStep, rule: MilestoneMatch): boolean {
  if (rule.kind !== undefined && step.kind !== rule.kind) return false;
  if (rule.toolName !== undefined && step.toolName !== rule.toolName) {
    return false;
  }
  const t = step.target.toLowerCase();
  const d = step.detail.toLowerCase();
  if (rule.targetContains !== undefined) {
    if (!t.includes(rule.targetContains.toLowerCase())) return false;
  }
  if (rule.targetEndsWith !== undefined) {
    if (!step.target.toLowerCase().endsWith(rule.targetEndsWith.toLowerCase())) {
      return false;
    }
  }
  if (rule.detailContains !== undefined) {
    if (!d.includes(rule.detailContains.toLowerCase())) return false;
  }
  return true;
}

function stepMatchesMilestone(step: TrajectoryStep, m: Milestone): boolean {
  const rules = singleMatch(m);
  if (rules.length === 0) return false;
  return rules.some((r) => matchOne(step, r));
}

export type PathLabel = "on_path" | "detour" | "neutral";

/**
 * Greedy sequential match against milestones → `on_path`.
 * Heuristic tags (`maybe-off-topic`, `self-hosted-ish`) → `detour` unless already `on_path`.
 */
export function labelStepsWithReference(
  steps: readonly TrajectoryStep[],
  ref: ReferenceTrajectoryFile,
): readonly PathLabel[] {
  const labels: PathLabel[] = steps.map(() => "neutral");
  let mi = 0;
  for (let si = 0; si < steps.length; si++) {
    const step = steps[si]!;
    if (
      mi < ref.milestones.length &&
      stepMatchesMilestone(step, ref.milestones[mi]!)
    ) {
      labels[si] = "on_path";
      mi += 1;
      continue;
    }
    if (
      step.tags.includes("maybe-off-topic") ||
      step.tags.includes("self-hosted-ish")
    ) {
      labels[si] = "detour";
    }
  }
  return labels;
}

export async function tryLoadReferenceTrajectory(
  scenarioId: string | undefined,
  evalRootDir: string,
): Promise<ReferenceTrajectoryFile | null> {
  if (!scenarioId) return null;
  const id = scenarioId.padStart(2, "0");
  const path = join(
    evalRootDir,
    "scenarios",
    "reference-trajectories",
    `${id}.json`,
  );
  try {
    const raw = await readFile(path, "utf8");
    const data = JSON.parse(raw) as ReferenceTrajectoryFile;
    if (data.scenarioId !== id && data.scenarioId !== scenarioId) {
      return null;
    }
    if (!Array.isArray(data.milestones)) return null;
    return data;
  } catch {
    return null;
  }
}

export function evalRootFromHere(importMetaUrl: string): string {
  return join(dirname(fileURLToPath(importMetaUrl)), "..");
}
