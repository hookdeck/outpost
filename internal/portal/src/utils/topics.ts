// Topic helpers. This module has no runtime imports so `npm test` can load it
// in plain Node.
import type { TopicDeprecation } from "../typings/Destination";

// parseTopicDeprecations keeps the deprecated topics of a v2 GET /topics body.
// Payload schemas can be large, so everything else is dropped before SWR caches
// the result. Malformed entries, or a v1 body (a list of names), are ignored.
export function parseTopicDeprecations(body: unknown): TopicDeprecation[] {
  if (!Array.isArray(body)) {
    return [];
  }
  const deprecations: TopicDeprecation[] = [];
  for (const entry of body) {
    if (typeof entry !== "object" || entry === null) {
      continue;
    }
    const { name, deprecated, replaced_by } = entry as Record<string, unknown>;
    if (typeof name !== "string" || name === "" || deprecated !== true) {
      continue;
    }
    deprecations.push(
      typeof replaced_by === "string" && replaced_by !== ""
        ? { name, replaced_by }
        : { name },
    );
  }
  return deprecations;
}

export function deprecationLabel(
  deprecation: TopicDeprecation | undefined,
): string | null {
  if (!deprecation) {
    return null;
  }
  return deprecation.replaced_by
    ? `Deprecated — use ${deprecation.replaced_by}`
    : "Deprecated";
}
