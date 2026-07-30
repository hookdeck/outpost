#!/usr/bin/env python3
"""
Archive a run's raw Prometheus data, at the resolution it was scraped at.

    python3 capture.py --prom http://localhost:9091 \
                       --artifact ../out/<run_id>.json --out ../out/<run_id>

Writes two files next to each other:

    <out>.raw.json.gz   every loadtest_* series over the whole run, 5s step,
                        native-histogram buckets intact
    <out>.ledger.json   the exact counters, from the app and from Prometheus

fetch.py already folds derived percentiles into the export, and that is what
the figures render from. This is the layer underneath: fetch.py decides 288
buckets and four quantiles at capture time, and those decisions cannot be
revisited later. A run costs a day and about a terabyte of egress, so the
distributions it produced are worth keeping in a form that can still answer a
question nobody thought to ask while it was running.

Native histograms come back from query_range with their buckets, so any
percentile — p999, or the shape of the tail — is recomputable from this file
long after Prometheus retention has rolled.

Two scopes are captured deliberately:

  - The raw series cover the entire run, warm-up and drain included. Warm-up is
    excluded from the *report* because it is not a measurement, which is not a
    reason to throw it away; how long the system took to settle is itself worth
    being able to look at.
  - The ledger is captured twice, per phase and in total, because the app's
    in-memory ledger is scoped to the steady window while the Prometheus
    counters are cumulative over the run. Neither is wrong; a reader comparing
    them without knowing that will think one of them is.
"""

import argparse
import gzip
import json
import sys
import urllib.parse
import urllib.request
from datetime import datetime, timedelta, timezone

# query_range returns at most 11,000 points per series. At a 5s step that is
# 15.2 hours, so a 24h run has to be fetched in pieces regardless; an hour per
# request also keeps any single response small enough to hold comfortably.
CHUNK = timedelta(hours=1)

# The run ends before its last scrape lands. Counters incremented in the final
# seconds — the cutoff sweep especially, which runs right at the end — are not
# visible to a query timed at ended_at, and reading there makes the ledger
# short by however many events those last increments covered. Verified: a local
# run read at ended_at was out by 2, and balanced exactly when read 30s later.
SETTLE = timedelta(seconds=60)

# The counters that make up the event accounting identity. published must equal
# completed + missing + cutoff, and a run where it does not is not a
# measurement — so these are the numbers the archive exists to preserve.
LEDGER_METRICS = [
    "loadtest_published_total",
    "loadtest_events_completed_total",
    "loadtest_delivered_total",
    "loadtest_missing_total",
    "loadtest_cutoff_total",
    "loadtest_recovered_total",
    "loadtest_duplicates_total",
    "loadtest_publish_errors_total",
    "loadtest_bytes_published_total",
]


def rfc3339(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00")).astimezone(timezone.utc)


class Prom:
    def __init__(self, base):
        self.base = base.rstrip("/")

    def _get(self, path, params):
        url = f"{self.base}{path}?{urllib.parse.urlencode(params)}"
        try:
            with urllib.request.urlopen(url, timeout=300) as r:
                body = json.load(r)
        except Exception as e:
            raise SystemExit(f"prometheus request failed: {e}\n  {params.get('query')}")
        if body.get("status") != "success":
            raise SystemExit(f"prometheus error: {body.get('error')}\n  {params.get('query')}")
        return body["data"]

    def range(self, query, start, end, step):
        return self._get("/api/v1/query_range", {
            "query": query,
            "start": start.isoformat().replace("+00:00", "Z"),
            "end": end.isoformat().replace("+00:00", "Z"),
            "step": f"{step}s",
        })["result"]

    def at(self, query, when):
        return self._get("/api/v1/query", {
            "query": query,
            "time": when.isoformat().replace("+00:00", "Z"),
        })["result"]


def fingerprint(metric):
    """A stable key for one series, so chunks of the same series concatenate
    rather than piling up as duplicates."""
    return json.dumps(metric, sort_keys=True)


def capture_series(prom, run_id, start, end, step):
    """Every series for this run, stitched back together across chunk
    boundaries.

    Two sources with two different scopes. The loadtest app labels everything
    with run_id, so the label does the scoping. Outpost's own metrics arrive
    over OTLP and know nothing about runs, so they can only be scoped by time
    — which means a concurrent run, or traffic from anything else pointed at
    the same deployment, lands in this archive too. The window is the only
    thing separating them.

    Outpost's series matter here because the benchmark Prometheus has no
    volume: it is the deployment's own account of the run, and if it is not
    archived at the end it is gone at the next restart.
    """
    selectors = [
        f'{{__name__=~"loadtest_.+", run_id="{run_id}"}}',
        '{__name__=~"outpost_.+"}',
    ]
    merged = {}
    chunks = 0

    cursor = start
    while cursor < end:
        stop = min(cursor + CHUNK, end)
        for selector in selectors:
            for s in prom.range(selector, cursor, stop, step):
                key = fingerprint(s["metric"])
                entry = merged.setdefault(key, {"metric": s["metric"]})
                # A series is either float samples or native histograms, never
                # both, but which one it is depends on the metric — so carry
                # whichever key Prometheus used rather than assuming.
                for field in ("values", "histograms"):
                    if field in s:
                        entry.setdefault(field, []).extend(s[field])
        chunks += 1
        print(f"    chunk {chunks}: {cursor:%H:%M} → {stop:%H:%M}  "
              f"({len(merged)} series so far)", file=sys.stderr)
        # Chunks are half-open on the left of the next one: query_range is
        # inclusive at both ends, so starting the next chunk at `stop` would
        # duplicate that sample in the stitched series.
        cursor = stop + timedelta(seconds=step)

    for entry in merged.values():
        for field in ("values", "histograms"):
            if field in entry:
                entry[field] = dedupe(entry[field])
    return list(merged.values())


def dedupe(points):
    """Drop repeated timestamps. Cheap insurance: a step that does not divide
    the chunk evenly can land the same scrape in two chunks."""
    seen, out = set(), []
    for p in points:
        t = p[0]
        if t not in seen:
            seen.add(t)
            out.append(p)
    return out


def capture_ledger(prom, run_id, at):
    """The counters as Prometheus holds them, broken out per profile and per
    phase. Read at the end of the run, when every counter has stopped moving.

    ABSENT means zero. Prometheus has no series for a counter that never
    incremented, so a profile with no missing events simply has no
    loadtest_missing_total — verified against runs that did have missing events,
    where the series is present and non-zero.
    """
    out = {}
    for metric in LEDGER_METRICS:
        key = metric.replace("loadtest_", "").replace("_total", "")
        by_profile = {}
        for s in prom.at(f'sum by (profile, phase) ({metric}{{run_id="{run_id}"}})', at):
            profile = s["metric"].get("profile", "?")
            phase = s["metric"].get("phase", "?")
            by_profile.setdefault(profile, {})[phase] = float(s["value"][1])
        out[key] = by_profile
    return out


def balance(ledger):
    """published == completed + missing + cutoff, per profile, over all phases.

    The whole point of the identity: the events that vanish from a run are the
    slow ones, so a run that quietly loses them reports an excellent p99 drawn
    from only the deliveries that made it.
    """
    def total(metric, profile):
        return sum(ledger.get(metric, {}).get(profile, {}).values())

    profiles = sorted(ledger.get("published", {}))
    rows = {}
    for p in profiles:
        pub = total("published", p)
        rest = total("events_completed", p) + total("missing", p) + total("cutoff", p)
        rows[p] = {
            "published": pub,
            "completed": total("events_completed", p),
            "missing": total("missing", p),
            "cutoff": total("cutoff", p),
            "remainder": pub - rest,
        }
    return rows


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--prom", default="http://localhost:9091")
    ap.add_argument("--artifact", required=True, help="run export written by the app")
    ap.add_argument("--out", help="output prefix (default: alongside the artifact)")
    ap.add_argument("--step", type=int, default=5,
                    help="sample resolution in seconds (default 5, the scrape interval)")
    args = ap.parse_args()

    with open(args.artifact) as f:
        art = json.load(f)

    run = art["run"]
    run_id = run["id"]
    start = rfc3339(run["started_at"])
    end = rfc3339(run["ended_at"]) if run.get("ended_at") else datetime.now(timezone.utc)
    if end <= start:
        raise SystemExit("run has no duration — nothing to capture")

    prefix = args.out or args.artifact.removesuffix(".json")
    prom = Prom(args.prom)

    # Both the series and the ledger read past the run's end so the final
    # scrape is included. Prometheus resolves a timestamp to the most recent
    # sample before it, so overshooting costs nothing and undershooting loses
    # the last few seconds of every counter.
    settled = end + SETTLE

    print(f"==> capturing {run_id} at {args.step}s "
          f"({(end - start).total_seconds() / 3600:.2f}h)", file=sys.stderr)
    series = capture_series(prom, run_id, start, settled, args.step)

    raw_path = f"{prefix}.raw.json.gz"
    with gzip.open(raw_path, "wt") as f:
        json.dump({
            "run_id": run_id,
            "start": start.isoformat().replace("+00:00", "Z"),
            "end": end.isoformat().replace("+00:00", "Z"),
            "step_seconds": args.step,
            "series": series,
        }, f)

    ledger = capture_ledger(prom, run_id, settled)
    rows = balance(ledger)
    ledger_path = f"{prefix}.ledger.json"
    with open(ledger_path, "w") as f:
        json.dump({
            "run_id": run_id,
            # Steady-window only, and the number the run was judged on.
            "app_steady": art.get("profiles", {}),
            "app_steady_total": art.get("total", {}),
            # Whole run, every phase, straight from the counters.
            "prometheus_by_phase": ledger,
            "prometheus_balance": rows,
        }, f, indent=2)

    points = sum(len(s.get("values", s.get("histograms", []))) for s in series)
    print(f"    {len(series)} series · {points:,} points → {raw_path}", file=sys.stderr)
    print(f"    ledger → {ledger_path}", file=sys.stderr)

    unbalanced = {p: r["remainder"] for p, r in rows.items() if r["remainder"]}
    if unbalanced:
        # Not fatal here. The app already decides whether a run is void; this
        # is a second opinion from an independent source, and it is worth
        # seeing when the two disagree.
        print(f"    warning: prometheus ledger does not balance: {unbalanced}",
              file=sys.stderr)
    if not series:
        print("    warning: no series captured — check the run_id and that "
              "Prometheus still has the run in retention", file=sys.stderr)


if __name__ == "__main__":
    main()
