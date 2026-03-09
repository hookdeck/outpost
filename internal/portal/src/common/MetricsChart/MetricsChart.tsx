import "./MetricsChart.scss";

import { useEffect, useRef, useState } from "react";
import {
  BarChart,
  Bar,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from "recharts";

// Replaces recharts' ResponsiveContainer which fires its internal ResizeObserver
// before flex layout resolves, measuring -1px and spamming console warnings.
// This hook waits for positive dimensions before we render the chart.
function useContainerSize(ref: React.RefObject<HTMLDivElement | null>) {
  const [size, setSize] = useState({ width: 0, height: 0 });

  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) {
        const { width, height } = entry.contentRect;
        setSize((prev) =>
          prev.width === width && prev.height === height
            ? prev
            : { width, height },
        );
      }
    });

    observer.observe(el);
    return () => observer.disconnect();
  }, [ref]);

  return size;
}

import { Loading } from "../Icons";

export type ChartDataPoint = Record<string, string | number> & {
  label: string;
};

export type ChartSeries = {
  key: string;
  label: string;
  cssVar: string;
};

interface MetricsChartProps {
  title: string;
  subtitle?: string;
  data: ChartDataPoint[];
  series: ChartSeries[];
  type: "bar" | "stacked-bar" | "line" | "multi-line";
  loading: boolean;
  error: boolean;
  yDomain?: [number, number];
  yTickFormatter?: (v: number) => string;
  yAllowDecimals?: boolean;
  tooltipFormatter?: (value: any, name: any) => [string, string];
}

function getYAxisWidth(
  data: ChartDataPoint[],
  series: ChartSeries[],
  formatter?: (v: number) => string,
): number {
  if (formatter) return 48;
  let max_value = 0;
  for (const d of data) {
    for (const s of series) {
      const v = Number(d[s.key] ?? 0);
      if (v > max_value) max_value = v;
    }
  }
  return 32 + String(Math.floor(max_value)).length * 8;
}

const CHART_MARGIN = { top: 0, right: 0, bottom: 0, left: 0 };

const TOOLTIP_STYLE = {
  background: "var(--colors-background)",
  border: "1px solid var(--colors-outline-neutral)",
  borderRadius: 6,
  fontSize: 13,
};

const MetricsChart: React.FC<MetricsChartProps> = ({
  title,
  subtitle,
  data,
  series,
  type,
  loading,
  error,
  yDomain,
  yTickFormatter,
  yAllowDecimals,
  tooltipFormatter,
}) => {
  const colorRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const [colors, setColors] = useState<Record<string, string>>({});
  const { width: chartWidth, height: chartHeight } = useContainerSize(bodyRef);
  const css_vars_key = series.map((s) => s.cssVar).join(",");

  useEffect(() => {
    if (colorRef.current) {
      const style = getComputedStyle(colorRef.current);
      const resolved: Record<string, string> = {};
      for (const v of css_vars_key.split(",")) {
        const val = style.getPropertyValue(v).trim();
        if (val) resolved[v] = val;
      }
      setColors(resolved);
    }
  }, [css_vars_key]);

  const getColor = (css_var: string) => colors[css_var] ?? "#888";

  const renderBody = () => {
    if (loading) {
      return (
        <div className="metrics-chart__loading">
          <Loading />
        </div>
      );
    }

    if (error) {
      return <div className="metrics-chart__error">Failed to load metrics</div>;
    }

    if (data.length === 0) {
      return <div className="metrics-chart__empty">No data</div>;
    }

    if (chartWidth <= 0 || chartHeight <= 0) {
      return null;
    }

    const tick_style = {
      fontSize: 12,
      fill: "var(--colors-foreground-neutral-2)",
    };

    const x_axis_props = {
      dataKey: "label" as const,
      stroke: "var(--colors-outline-neutral)",
      tick: tick_style,
      tickMargin: 8,
      minTickGap: 16,
    };

    const y_axis_props = {
      width: getYAxisWidth(data, series, yTickFormatter),
      allowDecimals: yAllowDecimals ?? false,
      stroke: "transparent",
      tick: tick_style,
      tickMargin: 8,
      minTickGap: 24,
      ...(yDomain ? { domain: yDomain } : {}),
      ...(yTickFormatter ? { tickFormatter: yTickFormatter } : {}),
    };

    const show_legend = type === "stacked-bar" || type === "multi-line";

    if (type === "bar" || type === "stacked-bar") {
      return (
        <BarChart width={chartWidth} height={chartHeight} data={data} margin={CHART_MARGIN} barCategoryGap="4%">
            <CartesianGrid
              strokeDasharray="3 3"
              vertical={false}
              stroke="var(--colors-outline-neutral)"
            />
            <XAxis {...x_axis_props} />
            <YAxis {...y_axis_props} />
            <Tooltip
              contentStyle={TOOLTIP_STYLE}
              labelFormatter={(label) => label}
              formatter={
                tooltipFormatter ??
                ((value: any, name: any) => [String(value), String(name)])
              }
            />
            {show_legend && (
              <Legend
                iconSize={8}
                wrapperStyle={{
                  fontSize: 12,
                  color: "var(--colors-foreground-neutral-2)",
                }}
              />
            )}
            {series.map((s) => (
              <Bar
                key={s.key}
                dataKey={s.key}
                name={s.label}
                fill={getColor(s.cssVar)}
                radius={[2, 2, 0, 0]}
                isAnimationActive={false}
                stackId={type === "stacked-bar" ? "stack" : undefined}
              />
            ))}
          </BarChart>
      );
    }

    return (
      <LineChart width={chartWidth} height={chartHeight} data={data} margin={CHART_MARGIN}>
          <CartesianGrid
            strokeDasharray="3 3"
            vertical={false}
            stroke="var(--colors-outline-neutral)"
          />
          <XAxis {...x_axis_props} />
          <YAxis {...y_axis_props} />
          <Tooltip
            contentStyle={TOOLTIP_STYLE}
            labelFormatter={(label) => label}
            formatter={
              tooltipFormatter ??
              ((value: any, name: any) => [String(value), String(name)])
            }
          />
          {show_legend && (
            <Legend
              iconSize={8}
              wrapperStyle={{
                fontSize: 12,
                color: "var(--colors-foreground-neutral-2)",
              }}
            />
          )}
          {series.map((s) => (
            <Line
              key={s.key}
              type="linear"
              dataKey={s.key}
              name={s.label}
              stroke={getColor(s.cssVar)}
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
          ))}
        </LineChart>
    );
  };

  return (
    <div className="metrics-chart" ref={colorRef}>
      <div className="metrics-chart__header">
        <span className="metrics-chart__title">
          {title}
          {subtitle && <span className="metrics-chart__subtitle"> / {subtitle}</span>}
        </span>
      </div>
      <div className="metrics-chart__body" ref={bodyRef}>{renderBody()}</div>
    </div>
  );
};

export default MetricsChart;
