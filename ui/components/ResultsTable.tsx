"use client";

import type { Point } from "@/lib/types";
import { msFromNs } from "@/lib/types";

export function ResultsTable({ points }: { points: Point[] }) {
  if (points.length === 0) return null;

  return (
    <div className="overflow-x-auto rounded-lg border border-neutral-200 dark:border-neutral-800">
      <table className="w-full border-collapse text-[12px]">
        <thead>
          <tr className="bg-neutral-50 text-left dark:bg-neutral-900">
            <th className="px-3 py-2 font-semibold">policy</th>
            <th className="px-3 py-2 text-right font-semibold">median delay</th>
            <th className="px-3 py-2 text-right font-semibold">p99 delay</th>
            <th className="px-3 py-2 text-right font-semibold">underruns</th>
            <th className="px-3 py-2 text-right font-semibold">worst run</th>
            <th className="px-3 py-2 text-right font-semibold">late</th>
            <th className="px-3 py-2 text-right font-semibold">resyncs</th>
          </tr>
        </thead>
        <tbody className="font-mono tabular-nums">
          {points.map((p) => (
            <tr
              key={p.policy}
              className={
                p.adaptive
                  ? "border-t border-neutral-200 bg-amber-50 dark:border-neutral-800 dark:bg-amber-950/30"
                  : "border-t border-neutral-200 dark:border-neutral-800"
              }
            >
              <td className="px-3 py-1.5 font-sans">{p.policy}</td>
              <td className="px-3 py-1.5 text-right">
                {msFromNs(p.medianPlayoutDelayNs.median).toFixed(1)}ms
              </td>
              <td className="px-3 py-1.5 text-right">
                {msFromNs(p.p99PlayoutDelayNs.median).toFixed(1)}ms
              </td>
              <td className="px-3 py-1.5 text-right">
                {(p.underrunRate.median * 100).toFixed(2)}%
                <span className="text-neutral-400">
                  {" "}
                  ({(p.underrunRate.min * 100).toFixed(2)}&ndash;
                  {(p.underrunRate.max * 100).toFixed(2)})
                </span>
              </td>
              <td className="px-3 py-1.5 text-right">{p.longestUnderrunRun.median.toFixed(0)}</td>
              <td className="px-3 py-1.5 text-right">{p.lateDiscards.median.toFixed(0)}</td>
              <td className="px-3 py-1.5 text-right">{p.resyncs.median.toFixed(0)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="border-t border-neutral-200 px-3 py-2 text-[11px] leading-relaxed text-neutral-500 dark:border-neutral-800">
        <strong>late</strong> counts packets that arrived after their slot and were discarded by the
        buffer&rsquo;s own delay choice &mdash; audio the network delivered fine and the buffer threw
        away. <strong>resyncs</strong> are playout schedule shifts, each audible in a real receiver
        and counted by neither the delay nor the underrun column.
      </p>
    </div>
  );
}
