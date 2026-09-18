import { useContext, useMemo } from "react";
import useSWR from "swr";
import { ApiContext } from "../../app";

export type Timeframe = "1h" | "24h" | "7d" | "30d";

export type MetricsDataPoint = {
  time_bucket: string;
  dimensions: Record<string, string>;
  metrics: Record<string, number>;
};

export type MetricsResponse = {
  data: MetricsDataPoint[];
  metadata: {
    granularity: string;
  };
};

// Round down to the nearest minute so the SWR key stays stable across renders
function roundToMinute(date: Date): Date {
  const d = new Date(date);
  d.setSeconds(0, 0);
  return d;
}

function getDateRange(timeframe: Timeframe) {
  const now = roundToMinute(new Date());
  const end = now.toISOString();
  const start = new Date(now);

  switch (timeframe) {
    case "1h":
      start.setHours(start.getHours() - 1);
      break;
    case "24h":
      start.setHours(start.getHours() - 24);
      break;
    case "7d":
      start.setDate(start.getDate() - 7);
      break;
    case "30d":
      start.setDate(start.getDate() - 30);
      break;
  }

  return { start: start.toISOString(), end };
}

function getGranularity(timeframe: Timeframe) {
  switch (timeframe) {
    case "1h":
      return "1m";
    case "24h":
      return "30m";
    case "7d":
      return "3h";
    case "30d":
      return "12h";
  }
}

function getTimeBucketCount(
  start: string,
  end: string,
  granularity: string,
): number {
  const match = granularity.match(/^(\d+)([smhdw])$/);
  if (!match) return 0;

  const unitMilliseconds: Record<string, number> = {
    s: 1_000,
    m: 60_000,
    h: 3_600_000,
    d: 86_400_000,
    w: 604_800_000,
  };
  const bucketSize = Number(match[1]) * unitMilliseconds[match[2]];
  const startTime = Date.parse(start);
  const endTime = Date.parse(end);
  const firstBucket = Math.floor(startTime / bucketSize) * bucketSize;

  return Math.max(0, Math.ceil((endTime - firstBucket) / bucketSize));
}

export function useMetrics({
  measures,
  destinationId,
  timeframe,
  dimensions,
  filters,
  granularity: granularityOverride,
}: {
  measures: string[];
  destinationId: string;
  timeframe: Timeframe;
  dimensions?: string[];
  filters?: Record<string, string>;
  granularity?: string;
}) {
  const apiClient = useContext(ApiContext);

  // Stable keys for useMemo deps
  const measuresKey = measures.join(",");
  const dimensionsKey = dimensions?.join(",") ?? "";
  const filtersKey = filters
    ? Object.entries(filters)
        .map(([k, v]) => `${k}=${v}`)
        .join(",")
    : "";

  const url = useMemo(() => {
    const { start, end } = getDateRange(timeframe);

    const params = new URLSearchParams();
    params.set("time[start]", start);
    params.set("time[end]", end);
    params.set("filters[destination_id]", destinationId);
    for (const m of measures) {
      params.append("measures[]", m);
    }

    if (filters) {
      for (const [k, v] of Object.entries(filters)) {
        params.set(`filters[${k}]`, v);
      }
    }

    if (dimensions && dimensions.length > 0) {
      // Aggregate query — no granularity
      for (const d of dimensions) {
        params.append("dimensions[]", d);
      }
    } else {
      // Time-series query — include granularity
      params.set(
        "granularity",
        granularityOverride ?? getGranularity(timeframe),
      );
    }

    return `metrics/attempts?${params.toString()}`;
  }, [
    measuresKey,
    dimensionsKey,
    filtersKey,
    granularityOverride,
    destinationId,
    timeframe,
  ]);

  const { data, error, isLoading } = useSWR<MetricsResponse>(
    url,
    (path: string) => apiClient.fetchRoot(path),
    {
      refreshInterval: 60_000,
      revalidateOnFocus: false,
    },
  );

  return { data, error, isLoading };
}

export function useBatchedMetrics({
  measures,
  destinationIds,
  timeframe,
  filters,
  granularity: granularityOverride,
}: {
  measures: string[];
  destinationIds: string[];
  timeframe: Timeframe;
  filters?: Record<string, string>;
  granularity?: string;
}) {
  const apiClient = useContext(ApiContext);

  const measuresKey = measures.join(",");
  const filtersKey = filters
    ? Object.entries(filters)
        .map(([k, v]) => `${k}=${v}`)
        .join(",")
    : "";
  const idsKey = [...destinationIds].sort().join(",");

  const request = useMemo(() => {
    if (destinationIds.length === 0) {
      return { url: null, sampleCount: 0 };
    }

    const { start, end } = getDateRange(timeframe);
    const granularity = granularityOverride ?? getGranularity(timeframe);
    const params = new URLSearchParams();
    params.set("time[start]", start);
    params.set("time[end]", end);

    for (const m of measures) {
      params.append("measures[]", m);
    }

    const sortedIds = [...destinationIds].sort();
    for (const id of sortedIds) {
      params.append("filters[destination_id][]", id);
    }

    params.append("dimensions[]", "destination_id");

    if (filters) {
      for (const [k, v] of Object.entries(filters)) {
        params.set(`filters[${k}]`, v);
      }
    }

    params.set("granularity", granularity);

    return {
      url: `metrics/attempts?${params.toString()}`,
      sampleCount: getTimeBucketCount(start, end, granularity),
    };
  }, [idsKey, measuresKey, filtersKey, granularityOverride, timeframe]);

  const { data, error, isLoading } = useSWR<MetricsResponse>(
    request.url,
    (path: string) => apiClient.fetchRoot(path),
    {
      refreshInterval: 60_000,
      revalidateOnFocus: false,
    },
  );

  const grouped = useMemo(() => {
    if (!data) return undefined;

    const result: Record<string, MetricsDataPoint[]> = {};
    for (const point of data.data) {
      const destId = point.dimensions.destination_id;
      if (!result[destId]) {
        result[destId] = [];
      }
      result[destId].push(point);
    }
    return result;
  }, [data]);

  const sampleCount = useMemo(() => {
    if (!data) return request.sampleCount;

    const responseBucketCount = new Set(
      data.data.map((point) => point.time_bucket),
    ).size;
    return responseBucketCount || request.sampleCount;
  }, [data, request.sampleCount]);

  return { data: grouped, error, isLoading, sampleCount };
}
