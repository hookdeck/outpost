#!/usr/bin/env python3
"""
Pull a run's time series out of Prometheus and fold them into its export.

    python3 fetch.py --prom http://localhost:9091 \
                     --artifact ../out/<run_id>.json --out ../out/<run_id>.json

The export the app writes holds the spec, the run identity, and the exact
ledger. This adds the per-bucket distributions, after which the file is
self-contained: figures render from it, never from a live query, so a published
number stays reproducible once Prometheus retention rolls.

Only the steady window is fetched. Warm-up is discarded by construction — it is
a different phase label, not a time range that has to be trimmed by hand.
"""

import argparse
import json
import sys
import urllib.parse
import urllib.request
from datetime import datetime, timezone

# Percentiles come from native histograms. On classic buckets
# histogram_quantile interpolates within a bucket, and at p99 that error is
# larger than the differences these sweeps exist to show.
QUANTILES = {
    "pub_p50": ("loadtest_publish_latency_seconds", 0.50),
    "pub_p99": ("loadtest_publish_latency_seconds", 0.99),
    "del_p50": ("loadtest_delivery_latency_seconds", 0.50),
    "del_p99": ("loadtest_delivery_latency_seconds", 0.99),
    "lag_p99": ("loadtest_generator_lag_seconds", 0.99),
}


def rfc3339(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00")).astimezone(timezone.utc)


class Prom:
    def __init__(self, base):
        self.base = base.rstrip("/")

    def range(self, query, start, end, step):
        """start/end are epoch seconds — the same grid the series align to."""
        params = urllib.parse.urlencode({
            "query": query,
            "start": start,
            "end": end,
            "step": f"{step}s",
        })
        url = f"{self.base}/api/v1/query_range?{params}"
        try:
            with urllib.request.urlopen(url, timeout=120) as r:
                body = json.load(r)
        except Exception as e:
            raise SystemExit(f"prometheus query failed: {e}\n  {query}")
        if body.get("status") != "success":
            raise SystemExit(f"prometheus error: {body.get('error')}\n  {query}")
        result = body["data"]["result"]
        return result[0]["values"] if result else []


def align(values, grid):
    """Put a Prometheus result onto the shared time grid, so every profile's
    array is the same length and the panels can zip them together."""
    lookup = {int(float(t)): float(v) for t, v in values if v not in ("NaN", None)}
    out, last = [], 0.0
    for t in grid:
        last = lookup.get(t, last)
        out.append(last)
    return out


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--prom", default="http://localhost:9091")
    ap.add_argument("--artifact", required=True, help="run export written by the app")
    ap.add_argument("--out", help="where to write the enriched export (default: in place)")
    ap.add_argument("--buckets", type=int, default=288,
                    help="target number of time buckets (default 288)")
    args = ap.parse_args()

    with open(args.artifact) as f:
        art = json.load(f)

    run = art["run"]
    run_id = run["id"]
    start = rfc3339(art["steady_start"])
    end = rfc3339(art["steady_end"])
    span = (end - start).total_seconds()
    if span <= 0:
        raise SystemExit("steady window is empty — the run did not reach steady state")

    # One bucket must hold enough samples for a p99 to mean anything, and the
    # rate window has to be at least a couple of scrape intervals wide.
    step = max(15, int(span / args.buckets))
    window_s = max(60, step * 3)
    rate_window = f"{window_s}s"

    # The first sample needs a full rate window behind it. Starting the grid at
    # steady_start opens every series with a zero that is an artifact of the
    # query, not a moment when the system published nothing.
    first = int(start.timestamp()) + window_s
    last = int(end.timestamp())
    if first > last:  # window shorter than the rate window: take what there is
        first = int(start.timestamp())
    grid = list(range(first, last + 1, step))
    hours = [(t - grid[0]) / 3600 for t in grid]

    prom = Prom(args.prom)
    profiles = {}

    for p in run["spec"]["profiles"]:
        name = p["name"]
        psel = f'{{run_id="{run_id}",profile="{name}",phase="steady"}}'
        series = {}
        for key, (metric, q) in QUANTILES.items():
            # No `by (le)`: native histograms carry their own buckets.
            query = f"histogram_quantile({q}, sum(rate({metric}{psel}[{rate_window}])))"
            series[key] = [v * 1000 for v in align(prom.range(query, grid[0], grid[-1], step), grid)]

        # Failures are cumulative from the start of the window: rare, bursty
        # events make a rate line noise, and a running total makes an incident
        # read as a step with its recovery visible.
        #
        # An event that delivered is not a failure, however it was counted along
        # the way. `missing` is written by the sweep and never revised, so a
        # delivery that arrives after the grace period stays counted there and
        # is added to `recovered` as well — subtracting is what makes the line
        # mean "never arrived". LT2 plotted 206 recoveries as failures because
        # this term was absent, in a run whose true delivery failures were zero.
        #
        # Publish errors are kept separate rather than summed in. They are a
        # different event: the publish call did not succeed, so the event never
        # entered the delivery path and no retry was ever owed. Adding them to
        # delivery failures produces a total that answers no question.
        fail = (f'sum(loadtest_missing_total{psel} or vector(0)) - '
                f'sum(loadtest_recovered_total{psel} or vector(0))')
        cum = align(prom.range(fail, grid[0], grid[-1], step), grid)
        base = cum[0] if cum else 0.0
        series["failed_cum"] = [max(0.0, v - base) for v in cum]

        perr = f'sum(loadtest_publish_errors_total{psel} or vector(0))'
        pcum = align(prom.range(perr, grid[0], grid[-1], step), grid)
        pbase = pcum[0] if pcum else 0.0
        series["publish_errors_cum"] = [max(0.0, v - pbase) for v in pcum]

        series["published"] = art.get("profiles", {}).get(name, {}).get("published", 0)
        profiles[name] = series

    # The headline latency panel reads this rather than a single profile.
    #
    # The quantile is taken over the summed histogram of every profile, not as
    # a mean of the per-profile quantiles. Percentiles do not average: the mean
    # of nine p99s is not the p99 of anything, and it would weight a 5 events/s
    # arm the same as a 640 events/s one. Summing the histograms first pools
    # the observations, so this is the p99 of all traffic in the run — each
    # profile counted by how many deliveries it actually contributed.
    #
    # It reads higher than the baseline profile alone, which is the intent.
    # Baseline is the fastest arm in the sheet, and quoting it as the run's
    # latency reports the best case as if it were the whole.
    allsel = f'{{run_id="{run_id}",phase="steady"}}'
    aggregate = {}
    for key, (metric, q) in QUANTILES.items():
        query = f"histogram_quantile({q}, sum(rate({metric}{allsel}[{rate_window}])))"
        aggregate[key] = [v * 1000 for v in
                          align(prom.range(query, grid[0], grid[-1], step), grid)]

    art["series"] = {
        "hours": hours,
        "step_seconds": step,
        "rate_window": rate_window,
        "aggregate": aggregate,
        "profiles": profiles,
    }
    art["offered_rate"] = sum(p["tenants"] * p["rate_per_tenant"]
                              for p in run["spec"]["profiles"])

    out = args.out or args.artifact
    with open(out, "w") as f:
        json.dump(art, f, indent=2)

    empty = [n for n, s in profiles.items() if not any(s["del_p99"])]
    if empty:
        print(f"warning: no delivery latency for {', '.join(empty)} — "
              "check the scrape target and that native histograms are enabled",
              file=sys.stderr)
    print(f"wrote {out}: {len(profiles)} profiles × {len(grid)} buckets "
          f"({step}s step over {span / 3600:.2f}h)")


if __name__ == "__main__":
    main()
