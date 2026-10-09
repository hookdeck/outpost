import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { deprecationLabel, parseTopicDeprecations } from "./topics.ts";

describe("parseTopicDeprecations", () => {
  it("keeps deprecated topics and their replacement only", () => {
    const body = [
      {
        name: "order.created",
        description: "Fires when an order is placed.",
        payload_schema: { type: "object", properties: {} },
        validation: "enforce",
        mcp: { enabled: true },
        deprecated: true,
        replaced_by: "order.created.v2",
      },
      { name: "order.created.v2", validation: "off", mcp: { enabled: true } },
      { name: "user.deleted", deprecated: true },
      { name: "user.created", deprecated: false, replaced_by: "x" },
    ];
    assert.deepEqual(parseTopicDeprecations(body), [
      { name: "order.created", replaced_by: "order.created.v2" },
      { name: "user.deleted" },
    ]);
  });

  it("ignores a body that isn't a list of topic objects", () => {
    assert.deepEqual(parseTopicDeprecations(undefined), []);
    assert.deepEqual(parseTopicDeprecations(null), []);
    assert.deepEqual(parseTopicDeprecations({ name: "a" }), []);
    assert.deepEqual(parseTopicDeprecations("a,b"), []);
    // The v1 shape: a list of names.
    assert.deepEqual(parseTopicDeprecations(["a", "b"]), []);
  });

  it("ignores malformed entries", () => {
    assert.deepEqual(
      parseTopicDeprecations([
        null,
        42,
        { deprecated: true },
        { name: "", deprecated: true },
        { name: 7, deprecated: true },
        { name: "a", deprecated: "true" },
        { name: "b", deprecated: true, replaced_by: 3 },
        { name: "c", deprecated: true, replaced_by: "" },
      ]),
      [{ name: "b" }, { name: "c" }],
    );
  });
});

describe("deprecationLabel", () => {
  it("names the replacement when there is one", () => {
    assert.equal(
      deprecationLabel({ name: "a", replaced_by: "a.v2" }),
      "Deprecated — use a.v2",
    );
    assert.equal(deprecationLabel({ name: "a" }), "Deprecated");
    assert.equal(deprecationLabel(undefined), null);
  });
});
