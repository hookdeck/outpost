import "./MetricsBreakdown.scss";

import { Loading } from "../Icons";
import { MetricsDataPoint } from "./useMetrics";

const VARIANT_CSS_VARS: Record<string, string> = {
  success: "var(--colors-dataviz-success)",
  error: "var(--colors-dataviz-error)",
};

function BreakdownBar({ width, variant }: { width: number; variant?: string }) {
  const bg = variant ? VARIANT_CSS_VARS[variant] : undefined;

  return (
    <div
      className="metrics-breakdown__bar"
      style={{
        width: `${width}%`,
        ...(bg ? { background: bg } : {}),
      }}
    />
  );
}

interface BreakdownRow {
  dimension: string;
  count: number;
  error_rate?: number;
}

interface MetricsBreakdownProps {
  title: string;
  subtitle?: string;
  data: MetricsDataPoint[] | undefined;
  dimensionKey: string;
  loading: boolean;
  error: boolean;
  showErrorRate?: boolean;
  barColor?: (dimension: string) => string | undefined;
}

function toRows(
  data: MetricsDataPoint[] | undefined,
  dimension_key: string,
): BreakdownRow[] {
  if (!data) return [];
  return data
    .map((d) => ({
      dimension: d.dimensions[dimension_key] ?? "unknown",
      count: d.metrics.count ?? 0,
      error_rate: d.metrics.error_rate,
    }))
    .sort((a, b) => b.count - a.count);
}

const MetricsBreakdown: React.FC<MetricsBreakdownProps> = ({
  title,
  subtitle,
  data,
  dimensionKey,
  loading,
  error,
  showErrorRate,
  barColor,
}) => {
  const rows = toRows(data, dimensionKey);
  const max_count = rows.reduce((max, r) => Math.max(max, r.count), 0);

  const renderBody = () => {
    if (loading) {
      return (
        <div className="metrics-breakdown__loading">
          <Loading />
        </div>
      );
    }

    if (error) {
      return (
        <div className="metrics-breakdown__empty">Failed to load metrics</div>
      );
    }

    if (rows.length === 0) {
      return <div className="metrics-breakdown__empty">No data</div>;
    }

    return (
      <div className="metrics-breakdown__rows">
        {rows.map((row) => (
          <div key={row.dimension} className="metrics-breakdown__row">
            <span className="metrics-breakdown__dimension">
              {row.dimension}
            </span>
            <div className="metrics-breakdown__bar-container">
              <BreakdownBar
                width={max_count > 0 ? (row.count / max_count) * 100 : 0}
                variant={barColor?.(row.dimension)}
              />
            </div>
            <span className="metrics-breakdown__count">{row.count}</span>
            {showErrorRate && row.error_rate != null && (
              <span className="metrics-breakdown__rate">
                {(row.error_rate * 100).toFixed(1)}%
              </span>
            )}
          </div>
        ))}
      </div>
    );
  };

  return (
    <div className="metrics-breakdown">
      <div className="metrics-breakdown__header">
        <span className="metrics-breakdown__title">
          {title}
          {subtitle && (
            <span className="metrics-breakdown__subtitle"> / {subtitle}</span>
          )}
        </span>
        {showErrorRate && (
          <div className="metrics-breakdown__column-headers">
            <span>Count</span>
            <span>Error %</span>
          </div>
        )}
      </div>
      {renderBody()}
    </div>
  );
};

export default MetricsBreakdown;
