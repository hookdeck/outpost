import Sparkline from "../../common/Sparkline/Sparkline";
import type { MetricsDataPoint } from "../../common/MetricsChart/useMetrics";

interface DestinationEventsCellProps {
  metricsData?: MetricsDataPoint[];
  isLoading: boolean;
}

// The destination list queries 24 hours at 4-hour granularity. The API
// includes both boundary buckets, so a complete sparkline has seven samples.
const EMPTY_SERIES_SAMPLE_COUNT = 7;

function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

const DestinationEventsCell: React.FC<DestinationEventsCellProps> = ({
  metricsData,
  isLoading,
}) => {
  if (isLoading || !metricsData) {
    return <span className="histogram-cell__loading"></span>;
  }

  const points =
    metricsData.length > 0
      ? metricsData.map((d) => ({
          successful: d.metrics.successful_count ?? 0,
          failed: d.metrics.failed_count ?? 0,
        }))
      : Array.from({ length: EMPTY_SERIES_SAMPLE_COUNT }, () => ({
          successful: 0,
          failed: 0,
        }));

  const total = points.reduce((sum, p) => sum + p.successful + p.failed, 0);

  return (
    <>
      <Sparkline data={points} />
      <span className="muted-variant">{formatCount(total)}</span>
    </>
  );
};

export default DestinationEventsCell;
