import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  chunkDestinationIds,
  groupByDestination,
  MAX_METRICS_DESTINATION_IDS,
} from "./metrics.ts";

const ids = (n: number) =>
  Array.from({ length: n }, (_, i) => `sub_${String(i).padStart(32, "0")}`);

describe("chunkDestinationIds", () => {
  it("returns no chunk for no IDs", () => {
    assert.deepEqual(chunkDestinationIds([]), []);
  });

  it("sorts the IDs so the requests don't depend on list order", () => {
    assert.deepEqual(chunkDestinationIds(["c", "a", "b"]), [["a", "b", "c"]]);
  });

  it("splits the IDs into chunks of at most the limit", () => {
    const all = ids(120);
    const chunks = chunkDestinationIds([...all].reverse());
    assert.deepEqual(
      chunks.map((chunk) => chunk.length),
      [50, 50, 20],
    );
    assert.equal(MAX_METRICS_DESTINATION_IDS, 50);
    assert.deepEqual(chunks.flat(), all);
  });

  it("takes a custom chunk size", () => {
    assert.deepEqual(chunkDestinationIds(["a", "b", "c"], 2), [
      ["a", "b"],
      ["c"],
    ]);
  });
});

describe("groupByDestination", () => {
  it("merges the points of every response by destination ID", () => {
    const point = (destination_id: string, time_bucket: string) => ({
      time_bucket,
      dimensions: { destination_id },
      metrics: { successful_count: 1 },
    });
    const grouped = groupByDestination([
      { data: [point("a", "t1"), point("b", "t1"), point("a", "t2")] },
      { data: [point("c", "t1")] },
      { data: [] },
    ]);
    assert.deepEqual(grouped, {
      a: [point("a", "t1"), point("a", "t2")],
      b: [point("b", "t1")],
      c: [point("c", "t1")],
    });
  });
});
