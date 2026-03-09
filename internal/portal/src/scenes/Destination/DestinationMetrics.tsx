import { useState } from "react";
import MetricsChart, {
  ChartDataPoint,
} from "../../common/MetricsChart/MetricsChart";
import MetricsBreakdown from "../../common/MetricsChart/MetricsBreakdown";
import { Timeframe, useMetrics } from "../../common/MetricsChart/useMetrics";

const TIMEFRAMES: Timeframe[] = ["1h", "24h", "7d", "30d"];

function formatLabel(iso: string, timeframe: Timeframe): string {
  const d = new Date(iso);
  if (timeframe === "1h" || timeframe === "24h") {
    return d.toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    });
  }
  return d.toLocaleDateString([], { month: "short", day: "numeric" });
}

function toTimeSeriesData(
  data: { time_bucket: string; metrics: Record<string, number> }[] | undefined,
  keys: string[],
  timeframe: Timeframe,
): ChartDataPoint[] {
  if (!data) return [];
  return data.map((d) => {
    const point: ChartDataPoint = {
      label: formatLabel(d.time_bucket, timeframe),
    };
    for (const k of keys) {
      point[k] = d.metrics[k] ?? 0;
    }
    return point;
  });
}

function hasActivity(
  data: { metrics: Record<string, number> }[] | undefined,
): boolean {
  if (!data || data.length === 0) return false;
  return data.some(
    (d) =>
      (d.metrics.successful_count ?? 0) + (d.metrics.failed_count ?? 0) > 0,
  );
}

interface DestinationMetricsProps {
  destinationId: string;
}

const DestinationMetrics: React.FC<DestinationMetricsProps> = ({
  destinationId,
}) => {
  const [timeframe, setTimeframe] = useState<Timeframe>("24h");

  // Row 1
  const event_count = useMetrics({
    measures: ["count"],
    destinationId,
    timeframe,
    filters: { attempt_number: "0" },
  });

  const delivery = useMetrics({
    measures: ["successful_count", "failed_count"],
    destinationId,
    timeframe,
  });

  // Derive from delivery data — if no deliveries happened, all time-series are empty
  const has_activity = hasActivity(delivery.data?.data);

  // Row 2
  const error_rate = useMetrics({
    measures: ["error_rate"],
    destinationId,
    timeframe,
  });

  const by_status = useMetrics({
    measures: ["count"],
    destinationId,
    timeframe,
    dimensions: ["code"],
  });

  const by_topic = useMetrics({
    measures: ["count", "error_rate"],
    destinationId,
    timeframe,
    dimensions: ["topic"],
  });

  // Row 3
  const retries = useMetrics({
    measures: ["first_attempt_count", "retry_count"],
    destinationId,
    timeframe,
  });

  const avg_attempt = useMetrics({
    measures: ["avg_attempt_number"],
    destinationId,
    timeframe,
  });

  return (
    <div className="metrics-container">
      <div className="metrics-container__header">
        <h2 className="title-l">Metrics</h2>
        <div className="metrics-container__timeframe">
          {TIMEFRAMES.map((tf) => (
            <button
              key={tf}
              className={`metrics-container__tf-btn ${timeframe === tf ? "metrics-container__tf-btn--active" : ""}`}
              onClick={() => setTimeframe(tf)}
            >
              {tf}
            </button>
          ))}
        </div>
      </div>
      <div className="metrics-container__grid">
        {/* Row 1 — Volume */}
        <div className="metrics-container__cell">
          <MetricsChart
            title="Events"
            subtitle="count"
            type="bar"
            series={[
              {
                key: "count",
                label: "Events",
                cssVar: "--colors-dataviz-info",
              },
            ]}
            data={
              has_activity
                ? toTimeSeriesData(
                    event_count.data?.data,
                    ["count"],
                    timeframe,
                  )
                : []
            }
            loading={event_count.isLoading}
            error={!!event_count.error}
          />
        </div>
        <div className="metrics-container__cell">
          <MetricsChart
            title="Event deliveries"
            subtitle="count"
            type="stacked-bar"
            series={[
              {
                key: "successful_count",
                label: "Success",
                cssVar: "--colors-dataviz-success",
              },
              {
                key: "failed_count",
                label: "Failed",
                cssVar: "--colors-dataviz-error",
              },
            ]}
            data={
              has_activity
                ? toTimeSeriesData(
                    delivery.data?.data,
                    ["successful_count", "failed_count"],
                    timeframe,
                  )
                : []
            }
            loading={delivery.isLoading}
            error={!!delivery.error}
          />
        </div>

        {/* Row 2 — Error Breakdown (3 columns) */}
        <div className="metrics-container__cell metrics-container__cell--row2">
          <MetricsChart
            title="Errors"
            subtitle="rate"
            type="line"
            series={[
              {
                key: "error_rate",
                label: "Error rate",
                cssVar: "--colors-dataviz-error",
              },
            ]}
            data={
              has_activity
                ? toTimeSeriesData(
                    error_rate.data?.data,
                    ["error_rate"],
                    timeframe,
                  )
                : []
            }
            loading={error_rate.isLoading}
            error={!!error_rate.error}
            yDomain={[0, 1]}
            yTickFormatter={(v) => `${(v * 100).toFixed(0)}%`}
            yAllowDecimals={true}
            tooltipFormatter={(value) => [
              `${(Number(value) * 100).toFixed(1)}%`,
              "Error rate",
            ]}
          />
        </div>
        <div className="metrics-container__cell metrics-container__cell--row2">
          <MetricsBreakdown
            title="By status code"
            data={by_status.data?.data}
            dimensionKey="code"
            loading={by_status.isLoading}
            error={!!by_status.error}
            barColor={(code) => {
              const n = Number(code);
              if (n >= 200 && n < 300) return "success";
              if (n >= 400) return "error";
              return undefined;
            }}
          />
        </div>
        <div className="metrics-container__cell metrics-container__cell--row2">
          <MetricsBreakdown
            title="By topic"
            data={by_topic.data?.data}
            dimensionKey="topic"
            loading={by_topic.isLoading}
            error={!!by_topic.error}
            showErrorRate
          />
        </div>

        {/* Row 3 — Retry Pressure */}
        <div className="metrics-container__cell">
          <MetricsChart
            title="Retries"
            subtitle="count"
            type="multi-line"
            series={[
              {
                key: "first_attempt_count",
                label: "First attempt",
                cssVar: "--colors-dataviz-info",
              },
              {
                key: "retry_count",
                label: "Retry",
                cssVar: "--colors-dataviz-warning",
              },
            ]}
            data={
              has_activity
                ? toTimeSeriesData(
                    retries.data?.data,
                    ["first_attempt_count", "retry_count"],
                    timeframe,
                  )
                : []
            }
            loading={retries.isLoading}
            error={!!retries.error}
          />
        </div>
        <div className="metrics-container__cell">
          <MetricsChart
            title="Avg attempt number"
            subtitle="avg"
            type="line"
            series={[
              {
                key: "avg_attempt_number",
                label: "Avg attempt number",
                cssVar: "--colors-dataviz-info",
              },
            ]}
            data={
              has_activity
                ? toTimeSeriesData(
                    avg_attempt.data?.data,
                    ["avg_attempt_number"],
                    timeframe,
                  )
                : []
            }
            loading={avg_attempt.isLoading}
            error={!!avg_attempt.error}
            yAllowDecimals={true}
          />
        </div>
      </div>
    </div>
  );
};

export default DestinationMetrics;
