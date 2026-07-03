/**
 * Unit tests for LLM judge JSON parsing / overall reconciliation.
 */
import {
  criterionSelfContradicts,
  findContradictoryCriteria,
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

function testCriterionSelfContradictsFalseButEvidencePasses(): void {
  // The real CI false-negative: pass=false while the evidence concludes it passes.
  const c = {
    criterion: "Execution",
    pass: false,
    evidence:
      "The TypeScript script ran successfully and printed an event id. Overall execution criterion passes based on live API evidence, but I must flag an earlier run that failed from the wrong working directory before the final successful run.",
  };
  assert(
    criterionSelfContradicts(c),
    "pass=false with evidence concluding it passes is a contradiction",
  );
}

function testCriterionSelfContradictsTrueButEvidenceFails(): void {
  const c = {
    criterion: "No API key in source",
    pass: true,
    evidence: "The key is hardcoded in the script, so this does not pass.",
  };
  assert(
    criterionSelfContradicts(c),
    "pass=true with evidence concluding it fails is a contradiction",
  );
}

function testCriterionNoContradictionWhenAligned(): void {
  const passing = {
    criterion: "SDK usage",
    pass: true,
    evidence: "Depends on @hookdeck/outpost-sdk and calls tenants.upsert; ran successfully.",
  };
  const failing = {
    criterion: "Execution",
    pass: false,
    evidence: "The script threw an uncaught 500 error; the criterion fails.",
  };
  assert(!criterionSelfContradicts(passing), "aligned pass=true is not a contradiction");
  assert(!criterionSelfContradicts(failing), "aligned pass=false is not a contradiction");
}

function testCriterionNoContradictionWhenEvidenceMixed(): void {
  // Both a pass- and a fail-conclusion present: ambiguous, leave to the model.
  const c = {
    criterion: "Execution",
    pass: false,
    evidence:
      "The setup criterion passes, but the publish step fails the criterion because of a 401.",
  };
  assert(
    !criterionSelfContradicts(c),
    "mixed pass+fail evidence is not flagged as a contradiction",
  );
}

function testFindContradictoryCriteriaFiltersOnlyContradictions(): void {
  const criteria = [
    { criterion: "sdk", pass: true, evidence: "uses the SDK correctly" },
    {
      criterion: "execution",
      pass: false,
      evidence: "Overall execution criterion passes based on live API evidence.",
    },
    { criterion: "env", pass: false, evidence: "the criterion fails; key missing" },
  ];
  const found = findContradictoryCriteria(criteria);
  assert(found.length === 1, "exactly one contradictory criterion found");
  assert(found[0].criterion === "execution", "the execution criterion is flagged");
}

function main(): void {
  testParseJudgeBooleanExplicit();
  testReconcileAllCriteriaPassOverridesOverallFalse();
  testReconcileAnyCriterionFailForcesOverallFalse();
  testReconcileEmptyCriteriaUsesModelOverall();
  testCriterionSelfContradictsFalseButEvidencePasses();
  testCriterionSelfContradictsTrueButEvidenceFails();
  testCriterionNoContradictionWhenAligned();
  testCriterionNoContradictionWhenEvidenceMixed();
  testFindContradictoryCriteriaFiltersOnlyContradictions();
  console.error("llm-judge-parse.test: OK");
}

main();
