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
      return "1h";
    case "7d":
      return "1d";
    case "30d":
      return "1d";
  }
}

export function useMetrics({
  measures,
  destinationId,
  timeframe,
  dimensions,
}: {
  measures: string[];
  destinationId: string;
  timeframe: Timeframe;
  dimensions?: string[];
}) {
  const apiClient = useContext(ApiContext);

  const measuresKey = measures.join(",");
  const dimensionsKey = dimensions?.join(",") ?? "";

  const url = useMemo(() => {
    const { start, end } = getDateRange(timeframe);

    const params = new URLSearchParams();
    params.set("date_range[start]", start);
    params.set("date_range[end]", end);
    params.set("filters[destination_id]", destinationId);
    for (const m of measuresKey.split(",")) {
      params.append("measures[]", m);
    }

    if (dimensionsKey) {
      // Aggregate query — no granularity
      for (const d of dimensionsKey.split(",")) {
        params.append("dimensions[]", d);
      }
    } else {
      // Time-series query — include granularity
      params.set("granularity", getGranularity(timeframe));
    }

    return `metrics/attempts?${params.toString()}`;
  }, [measuresKey, dimensionsKey, destinationId, timeframe]);

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
