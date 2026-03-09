import "./Sparkline.scss";

interface SparklineDataPoint {
  successful: number;
  failed: number;
}

interface SparklineProps {
  data: SparklineDataPoint[];
}

const Sparkline: React.FC<SparklineProps> = ({ data }) => {
  const max = data.reduce((m, d) => Math.max(m, d.successful + d.failed), 0);

  return (
    <div className="sparkline">
      {data.map((d, i) => {
        const total = d.successful + d.failed;

        if (total === 0) {
          return (
            <div key={i} className="sparkline__bar">
              <div className="sparkline__segment sparkline__segment--empty" />
            </div>
          );
        }

        const height = max > 0 ? (total / max) * 100 : 0;
        const failPct = (d.failed / total) * 100;
        const successPct = 100 - failPct;

        return (
          <div key={i} className="sparkline__bar" style={{ height: `${height}%` }}>
            {d.failed > 0 && (
              <div
                className="sparkline__segment sparkline__segment--error"
                style={{ height: `${failPct}%` }}
              />
            )}
            {d.successful > 0 && (
              <div
                className="sparkline__segment sparkline__segment--success"
                style={{ height: `${successPct}%` }}
              />
            )}
          </div>
        );
      })}
    </div>
  );
};

export default Sparkline;
