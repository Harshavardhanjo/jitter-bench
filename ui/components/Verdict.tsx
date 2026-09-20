"use client";

import type { Point } from "@/lib/types";
import { compare } from "@/lib/analysis";

const TONE: Record<string, string> = {
  "adaptive-better": "border-emerald-300 bg-emerald-50 dark:border-emerald-900 dark:bg-emerald-950/40",
  "adaptive-worse": "border-rose-300 bg-rose-50 dark:border-rose-900 dark:bg-rose-950/40",
  level: "border-neutral-300 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-900/60",
  unavailable: "border-neutral-300 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-900/60",
};

export function Verdict({ points }: { points: Point[] }) {
  if (points.length === 0) return null;

  const v = compare(points);

  return (
    <div className={`rounded-lg border p-4 ${TONE[v.outcome]}`}>
      <p className="text-sm font-semibold text-neutral-900 dark:text-neutral-100">{v.headline}</p>
      <p className="mt-1 text-[13px] leading-relaxed text-neutral-700 dark:text-neutral-300">
        {v.detail}
      </p>
    </div>
  );
}
