"use client";

import { useMemo } from "react";
import type { Point } from "@/lib/types";
import { msFromNs } from "@/lib/types";

// The chart plots the only two quantities that matter together: how much latency
// the buffer added, and how much audio the listener lost. A jitter buffer can
// only move along this curve, so a policy is good exactly insofar as it sits
// below and to the left of the alternatives.

type Props = {
  points: Point[];
};

// niceStep rounds a raw axis step up to 1, 2 or 5 times a power of ten, so tick
// labels read 0/50/100 rather than 0/50.2/100.4.
function niceStep(raw: number): number {
  if (raw <= 0) return 1;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const norm = raw / mag;
  const step = norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 5 ? 5 : 10;
  return step * mag;
}

function ticksUpTo(max: number, target: number): number[] {
  const step = niceStep(max / target);
  const out: number[] = [];
  for (let v = 0; v <= max + step / 2; v += step) out.push(v);
  return out;
}

const W = 760;
const H = 420;
const PAD = { top: 28, right: 24, bottom: 52, left: 62 };

type Plotted = {
  point: Point;
  x: number;
  y: number;
  yMin: number;
  yMax: number;
  delayMs: number;
  ratePct: number;
};

export function TradeoffChart({ points }: Props) {
  const model = useMemo(() => {
    if (points.length === 0) return null;

    const delaysMs = points.map((p) => msFromNs(p.medianPlayoutDelayNs.median));
    const rates = points.flatMap((p) => [p.underrunRate.min, p.underrunRate.max]);

    const xMax = Math.max(...delaysMs) * 1.08;
    // Give a perfect run a visible axis rather than a degenerate one.
    const yMaxRaw = Math.max(...rates);
    const yMax = yMaxRaw <= 0 ? 0.01 : yMaxRaw * 1.15;

    const plotW = W - PAD.left - PAD.right;
    const plotH = H - PAD.top - PAD.bottom;

    const sx = (ms: number) => PAD.left + (ms / xMax) * plotW;
    const sy = (rate: number) => PAD.top + plotH - (rate / yMax) * plotH;

    const plotted: Plotted[] = points.map((p) => {
      const delayMs = msFromNs(p.medianPlayoutDelayNs.median);
      return {
        point: p,
        x: sx(delayMs),
        y: sy(p.underrunRate.median),
        yMin: sy(p.underrunRate.min),
        yMax: sy(p.underrunRate.max),
        delayMs,
        ratePct: p.underrunRate.median * 100,
      };
    });

    const fixed = plotted
      .filter((p) => !p.point.adaptive)
      .sort((a, b) => a.delayMs - b.delayMs);
    const adaptive = plotted.find((p) => p.point.adaptive) ?? null;

    return {
      plotted,
      fixed,
      adaptive,
      sx,
      sy,
      xMax,
      yMax,
      plotH,
      xTickValues: ticksUpTo(xMax, 6),
      yTickValues: ticksUpTo(yMax, 5),
      // One decimal place is enough unless the whole axis lives under 1%.
      yDecimals: yMax * 100 < 1 ? 2 : 1,
    };
  }, [points]);

  if (!model) {
    return (
      <div className="flex h-[420px] items-center justify-center rounded-lg border border-neutral-200 text-sm text-neutral-500 dark:border-neutral-800">
        Run a sweep to draw the curve.
      </div>
    );
  }

  const { fixed, adaptive, sx, sy, xTickValues, yTickValues, yDecimals } = model;
  const curve = fixed.map((p) => `${p.x},${p.y}`).join(" ");

  return (
    <figure className="rounded-lg border border-neutral-200 bg-white p-2 dark:border-neutral-800 dark:bg-neutral-950">
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img"
        aria-label="Latency against underrun rate for each buffer policy">
        {/* gridlines and y axis */}
        {yTickValues.map((v) => (
          <g key={`y${v}`}>
            <line x1={PAD.left} x2={W - PAD.right} y1={sy(v)} y2={sy(v)}
              className="stroke-neutral-200 dark:stroke-neutral-800" strokeWidth={1} />
            <text x={PAD.left - 8} y={sy(v)} textAnchor="end" dominantBaseline="middle"
              className="fill-neutral-500 text-[11px]">
              {(v * 100).toFixed(yDecimals)}%
            </text>
          </g>
        ))}

        {/* x axis */}
        {xTickValues.map((v) => (
          <g key={`x${v}`}>
            <text x={sx(v)} y={H - PAD.bottom + 18} textAnchor="middle"
              className="fill-neutral-500 text-[11px]">
              {v.toFixed(0)}
            </text>
          </g>
        ))}

        <line x1={PAD.left} x2={W - PAD.right} y1={H - PAD.bottom} y2={H - PAD.bottom}
          className="stroke-neutral-400 dark:stroke-neutral-600" strokeWidth={1} />

        <text x={PAD.left + (W - PAD.left - PAD.right) / 2} y={H - 10} textAnchor="middle"
          className="fill-neutral-600 text-[12px] dark:fill-neutral-400">
          median playout delay (ms) — latency the listener pays
        </text>
        <text x={16} y={PAD.top + (H - PAD.top - PAD.bottom) / 2} textAnchor="middle"
          transform={`rotate(-90 16 ${PAD.top + (H - PAD.top - PAD.bottom) / 2})`}
          className="fill-neutral-600 text-[12px] dark:fill-neutral-400">
          underrun rate — audio the listener loses
        </text>

        {/* the reference line: where does the fixed curve reach adaptive's quality? */}
        {adaptive && (
          <line x1={PAD.left} x2={W - PAD.right} y1={adaptive.y} y2={adaptive.y}
            className="stroke-amber-500" strokeWidth={1} strokeDasharray="4 4" opacity={0.7} />
        )}

        {/* fixed-delay curve */}
        <polyline points={curve} fill="none" className="stroke-sky-600" strokeWidth={2} />

        {fixed.map((p) => (
          <g key={p.point.policy}>
            <line x1={p.x} x2={p.x} y1={p.yMin} y2={p.yMax}
              className="stroke-sky-700" strokeWidth={1} opacity={0.65} />
            <circle cx={p.x} cy={p.y} r={4} className="fill-sky-600" />
            <title>
              {`${p.point.policy}\ndelay ${p.delayMs.toFixed(1)}ms\nunderruns ${p.ratePct.toFixed(2)}% (${(p.point.underrunRate.min * 100).toFixed(2)}–${(p.point.underrunRate.max * 100).toFixed(2)})\nlate discards ${p.point.lateDiscards.median.toFixed(0)}`}
            </title>
          </g>
        ))}

        {/* adaptive */}
        {adaptive && (
          <g>
            <line x1={adaptive.x} x2={adaptive.x} y1={adaptive.yMin} y2={adaptive.yMax}
              className="stroke-amber-600" strokeWidth={1.5} />
            <rect x={adaptive.x - 5} y={adaptive.y - 5} width={10} height={10}
              transform={`rotate(45 ${adaptive.x} ${adaptive.y})`}
              className="fill-amber-500 stroke-amber-700" strokeWidth={1} />
            <text x={adaptive.x + 10} y={adaptive.y - 10}
              className="fill-amber-700 text-[11px] font-medium dark:fill-amber-400">
              adaptive
            </text>
            <title>
              {`adaptive\ndelay ${adaptive.delayMs.toFixed(1)}ms\nunderruns ${adaptive.ratePct.toFixed(2)}%\nresyncs ${adaptive.point.resyncs.median.toFixed(0)}`}
            </title>
          </g>
        )}
      </svg>

      <figcaption className="px-3 pb-2 pt-1 text-[12px] leading-relaxed text-neutral-600 dark:text-neutral-400">
        <span className="font-medium text-sky-700 dark:text-sky-400">Blue</span> traces fixed delays,
        low to high. <span className="font-medium text-amber-700 dark:text-amber-400">Amber</span> is
        the adaptive policy; the dashed line marks its underrun rate, so where it crosses the blue
        curve is the fixed delay that buys the same continuity. Vertical bars are the observed range
        across repetitions, not a confidence interval. Down and to the left is better.
      </figcaption>
    </figure>
  );
}
