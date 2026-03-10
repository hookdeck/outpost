import Sparkline from "../../common/Sparkline/Sparkline";
import { useMetrics } from "../../common/MetricsChart/useMetrics";

interface DestinationEventsCellProps {
  destinationId: string;
}

function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

const DestinationEventsCell: React.FC<DestinationEventsCellProps> = ({
  destinationId,
}) => {
  const { data, isLoading } = useMetrics({
    measures: ["successful_count", "failed_count"],
    destinationId,
    timeframe: "24h",
    granularity: "4h",
    filters: { attempt_number: "0" },
  });

  if (isLoading || !data) {
    return <span className="histogram-cell__loading"></span>;
  }

  const points = data.data.map((d) => ({
    successful: d.metrics.successful_count ?? 0,
    failed: d.metrics.failed_count ?? 0,
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
