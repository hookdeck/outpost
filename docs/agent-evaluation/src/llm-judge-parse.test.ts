/**
 * Unit tests for LLM judge JSON parsing / overall reconciliation.
 */
import { reconcileOverallTranscriptPass } from "./llm-judge.js";

function assert(condition: boolean, message: string): void {
  if (!condition) {
    throw new Error(message);
  }
}

function testReconcileAllCriteriaPassOverridesOverallFalse(): void {
  const criteria = [
    { criterion: "sdk", pass: true, evidence: "ok" },
    { criterion: "execution", pass: true, evidence: "ok" },
  ];
  assert(
    reconcileOverallTranscriptPass(false, criteria) === true,
    "all criteria pass => overall true even when model said false",
  );
}

function testReconcileAnyCriterionFailForcesOverallFalse(): void {
  const criteria = [
    { criterion: "sdk", pass: true, evidence: "ok" },
    { criterion: "execution", pass: false, evidence: "failed" },
  ];
  assert(
    reconcileOverallTranscriptPass(true, criteria) === false,
    "any criterion fail => overall false even when model said true",
  );
}

function testReconcileEmptyCriteriaUsesModelOverall(): void {
  assert(
    reconcileOverallTranscriptPass(true, []) === true,
    "no criteria => keep model overall true",
  );
  assert(
    reconcileOverallTranscriptPass(false, []) === false,
    "no criteria => keep model overall false",
  );
}

function main(): void {
  testReconcileAllCriteriaPassOverridesOverallFalse();
  testReconcileAnyCriterionFailForcesOverallFalse();
  testReconcileEmptyCriteriaUsesModelOverall();
  console.error("llm-judge-parse.test: OK");
}

main();
