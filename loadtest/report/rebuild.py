#!/usr/bin/env python3
"""
Rebuild a run's series block from its raw archive instead of from Prometheus.

    python3 rebuild.py --raw ../out/<run_id>.raw.json.gz \
                       --artifact ../out/<run_id>.json --out ../out/<run_id>.json

fetch.py can only read a live Prometheus, so a run's figures stop being
reproducible the moment retention rolls or the server is redeployed — the
benchmark Prometheus has no volume, so a restart is enough. capture.py already
writes every sample the panels need, native histograms included. This turns
that archive back into the same `series` block fetch.py produces, so the export
plus the archive is sufficient on its own.

Quantiles are computed the way Prometheus computes them over native histograms:
subtract the cumulative histogram one rate-window back, sum the buckets across
whatever series are in scope, then interpolate linearly inside the bucket that
holds the target rank.
"""

import argparse
import gzip
import json
from datetime import datetime, timezone

QUANTILES = {
    "pub_p50": ("loadtest_publish_latency_seconds", 0.50),
    "pub_p99": ("loadtest_publish_latency_seconds", 0.99),
    "del_p50": ("loadtest_delivery_latency_seconds", 0.50),
    "del_p99": ("loadtest_delivery_latency_seconds", 0.99),
    "lag_p99": ("loadtest_generator_lag_seconds", 0.99),
}


def rfc3339(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00")).astimezone(timezone.utc)


def bucket_map(h):
    """Native histogram buckets as {(lower, upper): count}.

    Prometheus emits them as [boundary_flag, lower, upper, count]. The flag
    only records which end is closed, which does not affect a sum or a rank,
    so the span alone is enough to line two histograms up.
    """
    out = {}
    for b in h.get("buckets") or []:
        _, lo, hi, cnt = b
        out[(float(lo), float(hi))] = out.get((float(lo), float(hi)), 0.0) + float(cnt)
    return out


def quantile(buckets, q):
    """Linear interpolation inside the bucket holding the q-th observation."""
    total = sum(buckets.values())
    if total <= 0:
        return 0.0
    target = q * total
    cum = 0.0
    for (lo, hi), cnt in sorted(buckets.items()):
        if cnt <= 0:
            continue
        if cum + cnt >= target:
            if cnt == 0:
                return hi
            return lo + (hi - lo) * ((target - cum) / cnt)
        cum += cnt
    return max(hi for hi, _ in [(k[1], v) for k, v in buckets.items()])


def series_index(raw):
    """Histogram series keyed by (metric name, profile), each a sorted list of
    (timestamp, bucket map). Only the steady phase — warm-up is a different
    phase label, so it is excluded by construction rather than by trimming."""
    idx = {}
    for s in raw["series"]:
        m = s["metric"]
        if m.get("phase") != "steady" or not s.get("histograms"):
            continue
        key = (m["__name__"], m.get("profile"))
        pts = idx.setdefault(key, [])
        for ts, h in s["histograms"]:
            pts.append((float(ts), bucket_map(h)))
    for pts in idx.values():
        pts.sort(key=lambda p: p[0])
    return idx


def at(pts, t):
    """Last sample at or before t, the way an instant query resolves."""
    lo, hi, found = 0, len(pts) - 1, None
    while lo <= hi:
        mid = (lo + hi) // 2
        if pts[mid][0] <= t:
            found = pts[mid]
            lo = mid + 1
        else:
            hi = mid - 1
    return found


def windowed(idx, metric, profiles, t, window):
    """Bucket counts accrued in the last `window` seconds, summed over profiles.

    Counters are cumulative, so this is the increase across the window — the
    same thing rate() measures, without dividing by time, since a quantile is
    scale-free.
    """
    total = {}
    for prof in profiles:
        pts = idx.get((metric, prof))
        if not pts:
            continue
        now, before = at(pts, t), at(pts, t - window)
        if not now:
            continue
        base = before[1] if before else {}
        for k, v in now[1].items():
            d = v - base.get(k, 0.0)
            if d > 0:
                total[k] = total.get(k, 0.0) + d
    return total


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--raw", required=True, help="<run_id>.raw.json.gz from capture.py")
    ap.add_argument("--artifact", required=True, help="run export written by the app")
    ap.add_argument("--out", help="where to write (default: in place)")
    ap.add_argument("--buckets", type=int, default=288)
    ap.add_argument("--aggregate-only", action="store_true",
                    help="add the pooled aggregate to an export that already has "
                         "per-profile series, leaving those series untouched")
    args = ap.parse_args()

    with open(args.artifact) as f:
        art = json.load(f)
    opener = gzip.open if args.raw.endswith(".gz") else open
    with opener(args.raw) as f:
        raw = json.load(f)

    run = art["run"]
    start, end = rfc3339(art["steady_start"]), rfc3339(art["steady_end"])
    span = (end - start).total_seconds()
    if span <= 0:
        raise SystemExit("steady window is empty — the run did not reach steady state")

    # Same grid arithmetic as fetch.py, so a rebuilt export is directly
    # comparable to one fetched live rather than merely similar.
    step = max(15, int(span / args.buckets))
    window = max(60, step * 3)
    first = int(start.timestamp()) + window
    last = int(end.timestamp())
    if first > last:
        first = int(start.timestamp())
    grid = list(range(first, last + 1, step))

    idx = series_index(raw)
    names = [p["name"] for p in run["spec"]["profiles"]]

    def quantiles(profiles):
        out = {}
        for key, (metric, q) in QUANTILES.items():
            out[key] = [quantile(windowed(idx, metric, profiles, t, window), q) * 1000
                        for t in grid]
        return out

    # Recomputing the per-profile series would overwrite numbers fetched live
    # with numbers derived a slightly different way, for no gain. Prometheus
    # extrapolates a rate to the edges of its window and this does not, which
    # is invisible on a pooled quantile over ~70k samples and very visible on a
    # 5 events/s arm where one window holds a few hundred. Adding only the
    # aggregate keeps an existing export's own values authoritative.
    if args.aggregate_only:
        block = art.get("series")
        if not block or "profiles" not in block:
            raise SystemExit("--aggregate-only needs an export that fetch.py has "
                             "already filled in")
        if block.get("step_seconds") != step:
            raise SystemExit(
                f"grid mismatch: export is on a {block.get('step_seconds')}s step, "
                f"this rebuild is on {step}s — the aggregate would not line up")
        block["aggregate"] = quantiles(names)
        out = args.out or args.artifact
        with open(out, "w") as f:
            json.dump(art, f, indent=2)
        print(f"added aggregate to {out} ({len(grid)} buckets, {step}s step)")
        return

    profiles = {}
    for name in names:
        s = quantiles([name])
        s["published"] = art.get("profiles", {}).get(name, {}).get("published", 0)
        # Failures come from the plain counters, which capture.py stores as
        # values rather than histograms.
        cum = []
        for metric in ("loadtest_publish_errors_total", "loadtest_missing_total"):
            for ser in raw["series"]:
                m = ser["metric"]
                if (m["__name__"] == metric and m.get("profile") == name
                        and m.get("phase") == "steady" and ser.get("values")):
                    pts = sorted((float(t), float(v)) for t, v in ser["values"])
                    cum.append(pts)
        def total_at(t):
            return sum((next((v for ts, v in reversed(p) if ts <= t), 0.0)) for p in cum)
        base = total_at(grid[0]) if grid else 0.0
        s["failed_cum"] = [max(0.0, total_at(t) - base) for t in grid]
        profiles[name] = s

    art["series"] = {
        "hours": [(t - grid[0]) / 3600 for t in grid],
        "step_seconds": step,
        "rate_window": f"{window}s",
        "aggregate": quantiles(names),
        "profiles": profiles,
    }
    art["offered_rate"] = sum(p["tenants"] * p["rate_per_tenant"]
                              for p in run["spec"]["profiles"])

    out = args.out or args.artifact
    with open(out, "w") as f:
        json.dump(art, f, indent=2)
    print(f"rebuilt {out} from {args.raw}: {len(profiles)} profiles × {len(grid)} "
          f"buckets ({step}s step over {span / 3600:.2f}h)")


if __name__ == "__main__":
    main()
