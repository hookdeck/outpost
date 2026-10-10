// Metrics request helpers. This module has no runtime imports so `npm test`
// can load it in plain Node.

// MAX_METRICS_DESTINATION_IDS bounds the destination IDs of one metrics
// request. Each ID adds about 70 bytes to the URL, and a tenant can have
// hundreds of MCP subscriptions: 50 keeps the URL under 4 KB, below the
// request line limits of common reverse proxies (8 KB for nginx).
export const MAX_METRICS_DESTINATION_IDS = 50;

// chunkDestinationIds sorts the IDs, so the requests don't change with the
// list order, and splits them into chunks of at most size IDs.
export function chunkDestinationIds(
  ids: readonly string[],
  size: number = MAX_METRICS_DESTINATION_IDS,
): string[][] {
  const sorted = [...ids].sort();
  const chunks: string[][] = [];
  for (let i = 0; i < sorted.length; i += size) {
    chunks.push(sorted.slice(i, i + size));
  }
  return chunks;
}

// groupByDestination merges the points of every response by their
// destination_id dimension.
export function groupByDestination<
  T extends { dimensions: Record<string, string> },
>(responses: readonly { data: readonly T[] }[]): Record<string, T[]> {
  const result: Record<string, T[]> = {};
  for (const response of responses) {
    for (const point of response.data) {
      const destId = point.dimensions.destination_id;
      if (!result[destId]) {
        result[destId] = [];
      }
      result[destId].push(point);
    }
  }
  return result;
}
