#!/usr/bin/env python3
"""
Outpost benchmark report figures, Hookdeck-branded.

Renders six panels, both as individual full-width files and composed into a single
sheet:

  01-measurement-model   what is measured, where the clocks are. Stable across runs.
  02-latency             publish + delivery p50/p99 over the window
  03-failure-rate        cumulative failed events per workload profile
  04-fanout              delivery + publish p99 by destinations per tenant
  05-payload             delivery + publish p99 by payload size
  06-receiver-response   delivery + publish p99 by receiver response time

  00-benchmark           all of the above, one file

Each panel draws into a Band, which maps panel-local figure coordinates into a slice
of whatever figure it is given. A standalone file is a Band covering the whole figure;
the composed sheet stacks the same bands at their natural heights, so panels are
identical in both outputs.

Palette and type come from website/src/styles/global.scss. Everything renders light
and dark.

Data comes from a run export:

    python3 make_charts.py --data ../out/<run_id>.json

With no --data the figures render synthetic placeholder shaped against the real
05-21 / 05-24 runs, and every results panel carries a SAMPLE DATA footer. That
footer is the only thing separating a layout preview from a published claim, so
it is driven by the data, never by hand.
"""

import argparse
import json
import os
import re
import numpy as np
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib import font_manager as fm
from matplotlib.lines import Line2D
from matplotlib.patches import FancyBboxPatch, FancyArrowPatch
from matplotlib.ticker import FuncFormatter

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", "..", ".."))
OUT = os.path.join(HERE, "charts")
os.makedirs(OUT, exist_ok=True)

fm.fontManager.addfont(os.path.join(ROOT, "website/public/fonts/Figtree-Bold.ttf"))
SANS = ["Helvetica Neue", "Helvetica", "Arial", "DejaVu Sans"]
MONO = ["Menlo", "SF Mono", "DejaVu Sans Mono"]

WIDTH = 11.0

# ---------------------------------------------------------------- brand tokens

LIGHT = dict(
    bg="#fafaf8", surface="#f5f5f5",
    fg1="#141412", fg2="#52504a", fg3="#7a786e",
    rule="#e0e0e0", grid="#ebebeb",
    publish="#0044cc", delivery="#006633", danger="#cc2314", warn="#997a00",
    ramp=["#b9cdf2", "#6f97e3", "#0044cc", "#00205e"],
)
DARK = dict(
    bg="#141412", surface="#1d1d1b",
    fg1="#f5f5f5", fg2="#cccccc", fg3="#7a7a7a",
    rule="#353533", grid="#272725",
    publish="#668fe0", delivery="#99ccb3", danger="#eba7a1", warn="#ffe066",
    ramp=["#24499e", "#4d7fd6", "#8fb3ef", "#cfe0fa"],
)

SAMPLE_NOTE = "SAMPLE DATA — illustrative layout, not measured results"
VOID_NOTE = "VOID RUN — preconditions not met, do not publish"


def style(T):
    plt.rcParams.update({
        "font.family": SANS, "font.size": 10,
        "figure.facecolor": T["bg"], "axes.facecolor": T["bg"],
        "savefig.facecolor": T["bg"], "text.color": T["fg1"],
        "axes.labelcolor": T["fg2"], "xtick.color": T["fg3"],
        "ytick.color": T["fg3"], "axes.edgecolor": T["rule"],
        "axes.linewidth": 0.8, "legend.frameon": False,
    })


class Band:
    """A horizontal slice of a figure, addressed in panel-local coordinates.

    y0 and h are fractions of the host figure. A panel written against a Band
    lays out identically whether it owns the figure or is stacked into a sheet.
    """

    def __init__(self, fig, y0=0.0, h=1.0, chrome=True):
        self.fig, self.y0, self.h = fig, y0, h
        self.chrome = chrome   # False when stacked: the sheet owns the footer

    def text(self, x, y, s, **kw):
        return self.fig.text(x, self.y0 + y * self.h, s, **kw)

    def axes(self, rect):
        left, bottom, width, height = rect
        return self.fig.add_axes(
            [left, self.y0 + bottom * self.h, width, height * self.h])


def frame(ax, T):
    for s in ("top", "right"):
        ax.spines[s].set_visible(False)
    for s in ("bottom", "left"):
        ax.spines[s].set_color(T["rule"])
    ax.grid(axis="y", color=T["grid"], lw=0.8, zorder=0)
    ax.set_axisbelow(True)
    ax.tick_params(length=3, labelsize=9)


def titleblock(B, T, title, subtitle, y=0.955):
    B.text(0.062, y, title, fontfamily="Figtree", fontsize=16.5,
           color=T["fg1"], va="top")
    B.text(0.062, y - 0.072, subtitle, fontsize=9.5, color=T["fg2"], va="top")


def notice(T):
    """The warning a results panel must carry, or None if it is publishable.

    Void beats sample: a real run that broke its own preconditions is more
    dangerous than an obvious mock, because the numbers look right."""
    if D.voids:
        return VOID_NOTE, T["danger"]
    if D.sample:
        return SAMPLE_NOTE, T["danger"]
    return None, None


def footer(B, T, left=None, sample=True):
    if not B.chrome:
        return
    B.text(0.062, 0.035, left or D.run_meta, fontfamily=MONO, fontsize=7.5,
           color=T["fg3"])
    text, color = notice(T)
    if sample and text:
        B.text(0.94, 0.035, text, fontfamily=MONO, fontsize=7.5,
               color=color, ha="right")


def hours_axis(ax):
    """X axis in elapsed hours. Ticks adapt to the window: a 10-minute pipeline
    check and a 24-hour run use the same panel code, and the axis says which
    one you are looking at."""
    span = HOURS[-1]
    ax.set_xlim(0, span)
    # Steps are round numbers of seconds rather than span/N, so no two labels
    # can round to the same string — a 3-minute window read "0m 0m 1m 1m" when
    # the step was a fraction of a minute.
    secs = span * 3600
    step_s = next((s for s in (10, 15, 30, 60, 120, 300, 600, 900, 1800,
                               3600, 7200, 10800, 21600, 43200)
                   if secs / s <= 8), 86400)
    if step_s < 60:
        fmt = lambda h: f"{h * 3600:.0f}s"
    elif step_s < 3600:
        fmt = lambda h: f"{h * 60:.0f}m"
    else:
        fmt = lambda h: f"{h:02.0f}:00"
    step = step_s / 3600
    ticks = np.arange(0, span + step / 2, step)
    ax.set_xticks(ticks)
    ax.set_xticklabels([fmt(h) for h in ticks])


def ms(v, _=None):
    return f"{v:.0f} ms"


# ========================================================================= data

class Dataset:
    """Everything the panels draw, from either a run export or the sample.

    Panels never branch on where the numbers came from. The one thing that does
    is the SAMPLE DATA footer, and it is set here rather than at a call site.
    """

    def __init__(self, hours, base, failures, sweeps, run_meta,
                 published_per_profile, sample, incident=None, voids=(),
                 latency_subtitle=None):
        self.hours = np.asarray(hours, dtype=float)
        self.base = base                    # dict of pub/del p50/p99 arrays
        self.failures = failures            # [(label, cumulative array)]
        self.sweeps = sweeps                # [(file, title, subtitle, {level: (del, pub)})]
        self.run_meta = run_meta
        self.published_per_profile = published_per_profile
        self.sample = sample
        self.incident = incident            # (hour, label) to annotate, or None
        self.voids = list(voids)
        # Describes the profile the latency panel actually plots. Read from the
        # spec for a real run, so the caption cannot claim a payload or a
        # response time the run did not use.
        self.latency_subtitle = latency_subtitle or (
            "Baseline profile · 1 destination · 6 KB payload · "
            "receiver responds in 250 ms")

    @property
    def latency_ymax(self):
        top = max(self.base[k].max() for k in self.base)
        return top * 1.25

    @property
    def sweep_ymax(self):
        top = 0.0
        for _, _, _, levels in self.sweeps:
            for dely, puby in levels.values():
                top = max(top, dely.max(), puby.max())
        return top * 1.25 if top else 1.0


# ------------------------------------------------------------ synthetic sample

RNG = np.random.default_rng(20260730)
SAMPLE_HOURS = np.linspace(0, 24, 288)  # 5-minute buckets
EVENTS_PER_BUCKET = 23_100              # ~77 events/s over a 5-minute bucket
HOURS = SAMPLE_HOURS                    # rebound once a dataset is chosen
PUBLISHED_PER_PROFILE = EVENTS_PER_BUCKET * SAMPLE_HOURS.size


def series(base, noise, drift=0.0, excursion=None):
    y = base + RNG.normal(0, noise * 0.6, HOURS.size)
    y = np.convolve(y, np.ones(3) / 3, mode="same")
    y[0], y[-1] = y[1], y[-2]
    y += np.linspace(0, drift, HOURS.size)
    y += base * 0.015 * np.sin(HOURS / 24 * 2 * np.pi * 3)
    if excursion:
        at, mag, width = excursion
        y += mag * np.exp(-((HOURS - at) ** 2) / (2 * width ** 2))
    return np.maximum(y, base * 0.35)


def failcum(rate_pct, spike=None):
    """Cumulative failed events. Failures are rare and bursty, so a rate line is
    mostly noise — the running total separates profiles and makes an incident
    read as a step, with recovery visible as the line going flat again."""
    p = np.abs(RNG.normal(rate_pct, rate_pct * 0.5, HOURS.size))
    if spike:
        at, mag, width = spike
        p += mag * np.exp(-((HOURS - at) ** 2) / (2 * width ** 2))
    return np.cumsum(EVENTS_PER_BUCKET * p / 100.0)


def sample_dataset():
    return Dataset(
        hours=SAMPLE_HOURS,
        base=dict(
            pub_p50=series(18, 1.1),
            pub_p99=series(42, 3.2, drift=1.5, excursion=(15.4, 26, 0.22)),
            del_p50=series(35, 2.0),
            del_p99=series(88, 6.0, drift=3.0, excursion=(15.4, 61, 0.24)),
        ),
        failures=[
            ("baseline", failcum(0.006, spike=(15.4, 1.6, 0.13))),
            ("10 s receiver", failcum(0.006)),
            ("20 destinations", failcum(0.017)),
            ("100 KB payload", failcum(0.030)),
        ],
        # each sweep level carries (delivery p99, publish p99)
        sweeps=[
            ("04-fanout", "Fan-out",
             "1000 events/s published · 6 KB payload · receiver responds in 250 ms",
             {"1":  (series(88, 6.0),             series(42, 3.0)),
              "5":  (series(112, 7.5),            series(44, 3.0)),
              "10": (series(148, 10.0, drift=6),  series(46, 3.2)),
              "20": (series(265, 21.0, drift=18), series(51, 3.6))}),
            ("05-payload", "Payload size",
             "1000 events/s · 1 destination · receiver responds in 250 ms",
             {"1 KB":   (series(68, 4.5),             series(31, 2.2)),
              "6 KB":   (series(88, 6.0),             series(42, 3.0)),
              "50 KB":  (series(165, 12.0, drift=7),  series(78, 5.5)),
              "100 KB": (series(268, 22.0, drift=16), series(128, 9.0))}),
            ("06-receiver-response", "Receiver response time",
             "1000 events/s · 1 destination · 6 KB payload",
             {"250 ms": (series(88, 6.0),             series(42, 3.0)),
              "1 s":    (series(112, 8.0, drift=4),   series(42, 3.0)),
              "5 s":    (series(172, 13.0, drift=9),  series(43, 3.1)),
              "10 s":   (series(258, 21.0, drift=17), series(45, 3.3))}),
        ],
        run_meta="outpost v1.1.0 · 1000 events/s sustained 24h · single deployment",
        published_per_profile=PUBLISHED_PER_PROFILE,
        sample=True,
        incident=(15.6, "receiver restart"),
    )


# --------------------------------------------------------------- run export

def _fmt_bytes(n):
    if n >= 1024 * 1024:
        return f"{n / 1024 / 1024:g} MB"
    if n >= 1024:
        return f"{n / 1024:g} KB"
    return f"{n} B"


def _fmt_ms(ms):
    return f"{ms / 1000:g} s" if ms >= 1000 else f"{ms:g} ms"


# The sweeps are not hardcoded: a profile belongs to a sweep when it differs
# from the baseline in exactly one dimension. That is the same one-factor-at-a-
# time rule the spec is written to, so the figures follow the spec rather than
# a naming convention that can silently drift out of step with it.
SWEEP_DIMS = [
    ("04-fanout", "Fan-out", "destinations_per_tenant", lambda v: f"{v:g}"),
    ("05-payload", "Payload size", "payload_bytes", _fmt_bytes),
    ("06-receiver-response", "Receiver response time", "response_ms", _fmt_ms),
]
DIM_KEYS = [d[2] for d in SWEEP_DIMS]


def _subtitle(base, exclude):
    parts = [f"{base['rate']:g} events/s"]
    if exclude != "destinations_per_tenant":
        n = base["destinations_per_tenant"]
        parts.append(f"{n:g} destination" + ("s" if n != 1 else ""))
    if exclude != "payload_bytes":
        parts.append(f"{_fmt_bytes(base['payload_bytes'])} payload")
    if exclude != "response_ms":
        parts.append(f"receiver responds in {_fmt_ms(base['response_ms'])}")
    return " · ".join(parts)


def load_dataset(path):
    with open(path) as f:
        art = json.load(f)

    series_block = art.get("series")
    if not series_block:
        raise SystemExit(
            f"{path} has no series — run fetch.py against Prometheus first")

    hours = np.asarray(series_block["hours"], dtype=float)
    per_profile = series_block["profiles"]
    spec = art["run"]["spec"]
    profiles = {p["name"]: p for p in spec["profiles"]}
    for name, p in profiles.items():
        p["rate"] = p["tenants"] * p["rate_per_tenant"]

    def arr(name, key):
        return np.asarray(per_profile[name][key], dtype=float)

    baseline = "baseline" if "baseline" in per_profile else next(iter(per_profile))
    base = {k: arr(baseline, k) for k in ("pub_p50", "pub_p99", "del_p50", "del_p99")}

    # Failures: the profiles that actually failed something, worst first. A
    # panel of flat zero lines says less than naming the ones that moved.
    fails = []
    for name in per_profile:
        y = np.asarray(per_profile[name].get("failed_cum", []), dtype=float)
        if y.size and y[-1] > 0:
            fails.append((name, y))
    fails.sort(key=lambda kv: kv[1][-1], reverse=True)
    if not fails:  # zero failures is itself the result — show the baseline flat
        fails = [(baseline, np.zeros_like(hours))]
    failures = fails[:4]

    sweeps = []
    b = profiles.get(baseline)
    if b:
        for fname, title, dim, fmt in SWEEP_DIMS:
            others = [dim2 for dim2 in DIM_KEYS if dim2 != dim]
            members = [b] + [
                p for n, p in profiles.items()
                if n != baseline and n in per_profile
                and p[dim] != b[dim]
                and all(p[o] == b[o] for o in others)
            ]
            if len(members) < 2:
                continue
            members.sort(key=lambda p: p[dim])
            levels = {fmt(p[dim]): (arr(p["name"], "del_p99"), arr(p["name"], "pub_p99"))
                      for p in members}
            sweeps.append((fname, title, _subtitle(b, dim), levels))

    total = art.get("total", {})
    run = art["run"]
    window = run["spec"].get("window", "")
    meta = " · ".join(x for x in [
        f"outpost {spec.get('version')}" if spec.get("version") else None,
        f"{total.get('published', 0):,} events over {window}",
        f"{art.get('offered_rate', spec_rate(spec)):g} events/s offered",
        "via api-outpost" if spec.get("target") == "gateway" else "direct to deployment",
        run["id"],
    ] if x)

    published = max((per_profile[n].get("published", 0) for n in per_profile),
                    default=0) or 1
    subtitle = (f"{baseline} profile · {_subtitle(b, None)}" if b else None)
    return Dataset(hours=hours, base=base, failures=failures, sweeps=sweeps,
                   run_meta=meta, published_per_profile=published,
                   sample=False, voids=run.get("voids") or [],
                   latency_subtitle=subtitle)


def spec_rate(spec):
    return sum(p["tenants"] * p["rate_per_tenant"] for p in spec["profiles"])


# ======================================================================= panels

def draw_model(B, T):
    ax = B.axes([0, 0, 1, 1])
    ax.set_xlim(0, 100)
    ax.set_ylim(0, 100)
    ax.axis("off")

    B.text(0.062, 0.965, "What the benchmark measures", fontfamily="Figtree",
           fontsize=17, color=T["fg1"], va="top")
    B.text(0.062, 0.917, "Every number in this report is derived from these four "
           "timestamps", fontsize=10, color=T["fg2"], va="top")

    ytop, h = 68, 12
    for x, w, label in ((3.0, 17.5, "Load\ngenerator"),
                        (25.5, 17.5, "Publish API"),
                        (48.0, 13.5, "Queue"),
                        (66.0, 15.5, "Delivery\nworker"),
                        (86.5, 11.0, "Destination")):
        ax.add_patch(FancyBboxPatch(
            (x, ytop), w, h, boxstyle="round,pad=0,rounding_size=1.2",
            facecolor=T["surface"], edgecolor=T["rule"], lw=1.1, zorder=2))
        ax.text(x + w / 2, ytop + h / 2, label, ha="center", va="center",
                fontsize=10, color=T["fg1"], zorder=3, linespacing=1.45)

    for x0, x1 in ((20.5, 25.5), (43.0, 48.0), (61.5, 66.0), (81.5, 86.5)):
        ax.add_patch(FancyArrowPatch(
            (x0, ytop + h / 2), (x1, ytop + h / 2),
            arrowstyle="-|>", mutation_scale=11, color=T["fg3"], lw=1.1, zorder=2))

    ax.add_patch(FancyArrowPatch(
        (34.0, ytop + h), (11.5, ytop + h), connectionstyle="arc3,rad=0.34",
        arrowstyle="-|>", mutation_scale=10, color=T["publish"], lw=1.1,
        ls=(0, (3, 2)), zorder=2))
    ax.text(23.0, 89.0, "2xx ack", ha="center", fontsize=8.5, color=T["publish"],
            fontfamily=MONO)

    ybase = 58
    ax.plot([6, 94], [ybase, ybase], color=T["rule"], lw=1.2, zorder=1)
    for x, t, label, c in (
        (10.0, "t0", "scheduled\nsend time", T["fg1"]),
        (30.0, "t1", "request\nwritten", T["fg3"]),
        (50.0, "t2", "2xx\nreceived", T["publish"]),
        (90.0, "t3", "first byte at\ndestination", T["delivery"]),
    ):
        ax.plot([x], [ybase], marker="o", ms=7, color=T["bg"],
                markeredgecolor=c, markeredgewidth=1.8, zorder=4)
        ax.text(x, ybase + 4.0, t, ha="center", fontsize=10, color=c,
                fontfamily=MONO, fontweight="bold")
        ax.text(x, ybase - 4.2, label, ha="center", va="top", fontsize=8.5,
                color=T["fg2"], linespacing=1.4)

    def bracket(y, x0, x1, color, label):
        ax.plot([x0, x0, x1, x1], [y + 1.6, y, y, y + 1.6], color=color, lw=1.3)
        ax.text((x0 + x1) / 2, y - 2.2, label, ha="center", va="top",
                fontsize=9.5, color=color, fontfamily=MONO)

    bracket(41.0, 10.0, 50.0, T["publish"], "Publish latency  =  t2 − t0")
    bracket(32.0, 10.0, 90.0, T["delivery"], "Delivery latency  =  t3 − t0")

    ax.text(50, 24.0,
            "Measured from the scheduled send time, not the actual send — "
            "a generator that falls behind still reports the delay it caused.",
            ha="center", va="center", fontsize=9, color=T["fg2"])

    ax.plot([6, 94], [20.0, 20.0], color=T["rule"], lw=0.9)
    ax.text(50, 15.5, "Every published event resolves to exactly one outcome, "
                      "and the three sum to the publish count",
            ha="center", va="center", fontsize=9, color=T["fg2"])
    for x, w, label, c in ((9, 22, "delivered 2xx", T["delivery"]),
                           (36, 26, "failed after retries", T["danger"]),
                           (67, 24, "in flight at cutoff", T["fg3"])):
        ax.add_patch(FancyBboxPatch(
            (x, 5.0), w, 6.5, boxstyle="round,pad=0,rounding_size=1.0",
            facecolor="none", edgecolor=c, lw=1.2, zorder=2))
        ax.text(x + w / 2, 8.25, label, ha="center", va="center", fontsize=9,
                color=c, fontfamily=MONO)
    for x in (33.5, 64.5):
        ax.text(x, 8.25, "+", ha="center", va="center", fontsize=11, color=T["fg3"])

    footer(B, T, "measurement model · stable across runs", sample=False)


def draw_latency(B, T):
    ax = B.axes([0.075, 0.145, 0.875, 0.605])
    titleblock(B, T, "Publish and delivery latency", D.latency_subtitle)

    for k, c, lbl, dash in (
        ("del_p99", T["delivery"], "Delivery p99", None),
        ("del_p50", T["delivery"], "Delivery p50", (0, (3, 2))),
        ("pub_p99", T["publish"], "Publish p99", None),
        ("pub_p50", T["publish"], "Publish p50", (0, (3, 2))),
    ):
        ax.plot(HOURS, BASE[k], color=c, lw=2.0 if dash is None else 1.3,
                ls="-" if dash is None else dash,
                alpha=1.0 if dash is None else 0.8, label=lbl, zorder=3)
    ax.fill_between(HOURS, BASE["del_p50"], BASE["del_p99"],
                    color=T["delivery"], alpha=0.07, zorder=1)

    frame(ax, T)
    hours_axis(ax)
    ax.set_ylim(0, D.latency_ymax)
    ax.yaxis.set_major_formatter(FuncFormatter(ms))
    ax.legend(ncol=4, loc="upper left", bbox_to_anchor=(0, 1.15), fontsize=9,
              handlelength=1.9, columnspacing=1.8)
    footer(B, T)


def draw_failures(B, T):
    ax = B.axes([0.075, 0.145, 0.735, 0.605])
    titleblock(B, T, "Failures",
               "Running total of publishes that did not deliver after retries, "
               "per workload profile")

    worst = max((y[-1] for _, y in FAILURES), default=0.0)
    for (lbl, y), c in zip(FAILURES, T["ramp"]):
        pct = y[-1] / PUBLISHED_PER_PROFILE * 100
        ax.plot(HOURS, y, color=c, lw=2.0, zorder=3)
        if worst == 0:
            continue  # the panel already says "no failed events"
        # Anchored just past the end of the data, not at hour 24 — short runs
        # pushed every label off the panel.
        ax.text(HOURS[-1] * 1.017, y[-1], f"{lbl}\n{y[-1]:,.0f}  ·  {pct:.3f}%",
                color=c, fontsize=9, va="center", fontfamily=MONO,
                linespacing=1.5)

    frame(ax, T)
    hours_axis(ax)
    if worst > 0:
        ax.set_ylim(0, worst * 1.25)
    else:
        # A clean run is a result. Say so, and keep the axis on whole events
        # instead of repeating "0" at every gridline.
        ax.set_ylim(0, 1)
        ax.set_yticks([0, 1])
        ax.text(0.5, 0.55, "no failed events", transform=ax.transAxes,
                ha="center", va="center", fontsize=10, color=T["fg3"],
                fontfamily=MONO)
    ax.yaxis.set_major_formatter(FuncFormatter(lambda v, _: f"{v:,.0f}"))
    ax.set_ylabel("Failed events, cumulative", fontsize=9)
    if D.incident:
        at, label = D.incident
        i = int(np.searchsorted(HOURS, at))
        y = FAILURES[0][1][min(i, len(FAILURES[0][1]) - 1)]
        ax.annotate(label, xy=(at, y), xytext=(at * 1.08, y * 1.45), fontsize=8.5,
                    color=T["fg3"],
                    arrowprops=dict(arrowstyle="-", color=T["fg3"], lw=0.7))
    footer(B, T)


def draw_sweep(B, T, title, subtitle, data):
    ax = B.axes([0.075, 0.145, 0.795, 0.605])
    titleblock(B, T, title, subtitle)

    for (lbl, (dely, puby)), c in zip(data.items(), T["ramp"]):
        ax.plot(HOURS, dely, color=c, lw=2.0, zorder=3)
        ax.plot(HOURS, puby, color=c, lw=1.2, ls=(0, (3, 2)), alpha=0.85, zorder=2)
        ax.text(24.4, dely[-1], lbl, color=c, fontsize=9.5, va="center",
                fontfamily=MONO, fontweight="bold")

    frame(ax, T)
    hours_axis(ax)
    ax.set_ylim(0, D.sweep_ymax)
    ax.yaxis.set_major_formatter(FuncFormatter(ms))
    ax.set_ylabel("Latency", fontsize=9)
    ax.legend(handles=[
        Line2D([], [], color=T["fg2"], lw=2.0, label="Delivery p99"),
        Line2D([], [], color=T["fg2"], lw=1.2, ls=(0, (3, 2)), label="Publish p99"),
    ], ncol=2, loc="upper left", bbox_to_anchor=(0, 1.15), fontsize=9,
        handlelength=1.9, columnspacing=1.8)
    footer(B, T)


# ---- panel registry: (filename, height in inches, draw callable)

def build_panels():
    panels = [("01-measurement-model", 6.6, draw_model),
              ("02-latency", 5.4, draw_latency),
              ("03-failure-rate", 5.4, draw_failures)]
    panels += [(name, 5.4, (lambda t, s, d: lambda B, T: draw_sweep(B, T, t, s, d))(
        title, subtitle, data)) for name, title, subtitle, data in D.sweeps]
    return panels


# ======================================================================== render

def render_single(T, tag, name, height, draw):
    fig = plt.figure(figsize=(WIDTH, height))
    draw(Band(fig), T)
    for ext in ("png", "svg"):
        fig.savefig(os.path.join(OUT, f"{name}-{tag}.{ext}"),
                    dpi=200 if ext == "png" else None)
    plt.close(fig)


def render_sheet(T, tag, PANELS):
    total = sum(h for _, h, _ in PANELS)
    fig = plt.figure(figsize=(WIDTH, total))
    y = 1.0
    for i, (_, height, draw) in enumerate(PANELS):
        frac = height / total
        y -= frac
        draw(Band(fig, y, frac, chrome=False), T)
        if i:  # hairline between panels
            fig.add_artist(Line2D([0.062, 0.94], [y + frac, y + frac],
                                  color=T["rule"], lw=0.8))
    pad = 0.10 / total   # one footer for the whole sheet
    fig.text(0.062, pad, D.run_meta, fontfamily=MONO, fontsize=8, color=T["fg3"])
    text, color = notice(T)
    if text:
        fig.text(0.94, pad, text, fontfamily=MONO, fontsize=8,
                 color=color, ha="right")
    for ext in ("png", "svg"):
        fig.savefig(os.path.join(OUT, f"00-benchmark-{tag}.{ext}"),
                    dpi=150 if ext == "png" else None)
    plt.close(fig)
    print(f"wrote 00-benchmark-{tag}  ({WIDTH:.0f}×{total:.1f} in, "
          f"{len(PANELS)} panels)")


def main():
    global D, HOURS, BASE, FAILURES, PUBLISHED_PER_PROFILE

    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--data", help="run export JSON; omit to render the sample")
    args = ap.parse_args()

    D = load_dataset(args.data) if args.data else sample_dataset()
    HOURS = D.hours
    BASE = D.base
    FAILURES = D.failures
    PUBLISHED_PER_PROFILE = D.published_per_profile

    if D.voids:
        print("VOID RUN — figures are marked and must not be published:")
        for v in D.voids:
            print(f"  - {v}")

    panels = build_panels()

    # Clear panels this renderer owns before writing. The set depends on the
    # spec — a run with no payload sweep emits no payload panel — so without
    # this a narrower run leaves a wider run's figures sitting in the directory,
    # undated and indistinguishable from its own output.
    stale = [f for f in os.listdir(OUT)
             if re.fullmatch(r"\d\d-[a-z-]+-(light|dark)\.(png|svg)", f)] \
        if os.path.isdir(OUT) else []
    for f in stale:
        os.remove(os.path.join(OUT, f))
    if stale:
        print(f"cleared {len(stale)} figure(s) from the previous run")

    for T, tag in ((LIGHT, "light"), (DARK, "dark")):
        style(T)
        for name, height, draw in panels:
            render_single(T, tag, name, height, draw)
        render_sheet(T, tag, panels)
        print(f"wrote {len(panels)} individual panels ({tag})")
    print(f"charts in {OUT}")


if __name__ == "__main__":
    main()
