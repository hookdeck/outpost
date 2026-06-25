/**
 * Unit tests for LLM judge JSON parsing / overall reconciliation.
 */
import {
  parseJudgeBooleanExplicit,
  reconcileOverallTranscriptPass,
} from "./llm-judge.js";

function assert(condition: boolean, message: string): void {
  if (!condition) {
    throw new Error(`Assertion failed: ${message}`);
  }
}

function testParseJudgeBooleanExplicit(): void {
  assert(parseJudgeBooleanExplicit(true).value === true, "boolean true");
  assert(parseJudgeBooleanExplicit(false).value === false, "boolean false");
  assert(parseJudgeBooleanExplicit("false").value === false, 'string "false"');
  assert(parseJudgeBooleanExplicit("FALSE").value === false, 'string "FALSE"');
  assert(parseJudgeBooleanExplicit("true").value === true, 'string "true"');
  assert(parseJudgeBooleanExplicit(undefined).value === false, "undefined => false");
  assert(!parseJudgeBooleanExplicit(undefined).explicit, "undefined not explicit");
  assert(parseJudgeBooleanExplicit("false").explicit, '"false" is explicit');
  assert(parseJudgeBooleanExplicit(true).explicit, "boolean is explicit");
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
  testParseJudgeBooleanExplicit();
  testReconcileAllCriteriaPassOverridesOverallFalse();
  testReconcileAnyCriterionFailForcesOverallFalse();
  testReconcileEmptyCriteriaUsesModelOverall();
  console.error("llm-judge-parse.test: OK");
}

main();
