// Turns a sweep into the one sentence worth reading.
//
// A chart shows where each policy landed. What a reader wants to know is whether
// adapting was worth it, and that question has a precise answer: find the
// cheapest fixed delay that achieves at least as much continuity as the adaptive
// policy did, and compare their latencies. If a fixed delay gets there for less,
// adaptation lost on this condition.

import type { Point } from "./types";
import { msFromNs } from "./types";

export type Verdict = {
  outcome: "adaptive-better" | "adaptive-worse" | "level" | "unavailable";
  headline: string;
  detail: string;
  adaptiveDelayMs: number;
  adaptiveRatePct: number;
  adaptiveResyncs: number;
  rivalPolicy?: string;
  rivalDelayMs?: number;
  rivalRatePct?: number;
};

/** Latency differences below this are noise, not a finding. */
const TOLERANCE_MS = 3;

export function compare(points: Point[]): Verdict {
  const adaptive = points.find((p) => p.adaptive);
  const fixed = points.filter((p) => !p.adaptive);

  if (!adaptive || fixed.length === 0) {
    return {
      outcome: "unavailable",
      headline: "Not enough data to compare.",
      detail: "A sweep needs the adaptive policy and at least one fixed delay.",
      adaptiveDelayMs: 0,
      adaptiveRatePct: 0,
      adaptiveResyncs: 0,
    };
  }

  const aDelay = msFromNs(adaptive.medianPlayoutDelayNs.median);
  const aRate = adaptive.underrunRate.median;
  const aResyncs = adaptive.resyncs.median;

  const base = {
    adaptiveDelayMs: aDelay,
    adaptiveRatePct: aRate * 100,
    adaptiveResyncs: aResyncs,
  };

  // Fixed delays that were at least as continuous as adaptive.
  const asGood = fixed
    .filter((p) => p.underrunRate.median <= aRate)
    .sort(
      (x, y) =>
        msFromNs(x.medianPlayoutDelayNs.median) - msFromNs(y.medianPlayoutDelayNs.median),
    );

  if (asGood.length === 0) {
    return {
      ...base,
      outcome: "adaptive-better",
      headline: `Adapting won: no fixed delay matched its ${(aRate * 100).toFixed(2)}% underrun rate.`,
      detail:
        aResyncs > 0
          ? `It held ${aDelay.toFixed(0)}ms of latency, and paid ${aResyncs.toFixed(0)} playout resyncs to do it — each one an audible discontinuity the underrun rate does not count.`
          : `It held ${aDelay.toFixed(0)}ms of latency without a single resync.`,
    };
  }

  const rival = asGood[0];
  const rDelay = msFromNs(rival.medianPlayoutDelayNs.median);
  const rRate = rival.underrunRate.median;

  const withRival = {
    ...base,
    rivalPolicy: rival.policy,
    rivalDelayMs: rDelay,
    rivalRatePct: rRate * 100,
  };

  const saving = aDelay - rDelay;

  if (saving > TOLERANCE_MS) {
    return {
      ...withRival,
      outcome: "adaptive-worse",
      headline: `Adapting lost: ${rival.policy} was at least as continuous for ${saving.toFixed(0)}ms less latency.`,
      detail:
        `Adaptive held ${aDelay.toFixed(0)}ms for ${(aRate * 100).toFixed(2)}% underruns; ` +
        `${rival.policy} held ${rDelay.toFixed(0)}ms for ${(rRate * 100).toFixed(2)}%. ` +
        (aResyncs > 0
          ? `Adaptive also spent ${aResyncs.toFixed(0)} resyncs, which a fixed delay never pays.`
          : `Adaptive spent no resyncs, so the cost was purely the extra latency.`),
    };
  }

  if (saving < -TOLERANCE_MS) {
    return {
      ...withRival,
      outcome: "adaptive-better",
      headline: `Adapting won: the cheapest fixed delay that was at least as continuous cost ${Math.abs(saving).toFixed(0)}ms more latency.`,
      detail:
        `Adaptive held ${aDelay.toFixed(0)}ms for ${(aRate * 100).toFixed(2)}% underruns against ` +
        `${rival.policy}'s ${rDelay.toFixed(0)}ms for ${(rRate * 100).toFixed(2)}%. ` +
        (aResyncs > 0
          ? `The ${aResyncs.toFixed(0)} resyncs it paid are the part of the bill this chart does not show.`
          : `It did so without a resync.`),
    };
  }

  return {
    ...withRival,
    outcome: "level",
    headline: `A draw: ${rival.policy} was at least as continuous within ${TOLERANCE_MS}ms of adaptive's latency.`,
    detail:
      aResyncs > 0
        ? `Adaptive gained nothing here and still paid ${aResyncs.toFixed(0)} resyncs, so on this condition the fixed delay is the better engineering choice.`
        : `Neither has an edge on this condition.`,
  };
}
