"use client";

import { useCallback, useEffect, useState } from "react";

import { Controls } from "@/components/Controls";
import { ResultsTable } from "@/components/ResultsTable";
import { TradeoffChart } from "@/components/TradeoffChart";
import { Verdict } from "@/components/Verdict";
import { getPresets, runSweep } from "@/lib/wasm";
import type { ParamsMs, Point, PresetInfo } from "@/lib/types";
import { paramsToMs } from "@/lib/types";

const DEFAULT_CONDITION = "wifi";

export default function Page() {
  const [presets, setPresets] = useState<PresetInfo[]>([]);
  const [selected, setSelected] = useState(DEFAULT_CONDITION);
  const [params, setParams] = useState<ParamsMs | null>(null);
  const [custom, setCustom] = useState(false);
  const [reps, setReps] = useState(3);
  const [count, setCount] = useState(1500);

  const [points, setPoints] = useState<Point[]>([]);
  const [running, setRunning] = useState(false);
  const [status, setStatus] = useState("Loading the simulator…");
  const [error, setError] = useState<string | null>(null);

  // Fetch the preset definitions from the wasm module, which also warms it up so
  // the first sweep is not paying for the download.
  useEffect(() => {
    let live = true;
    getPresets()
      .then((list) => {
        if (!live) return;
        setPresets(list);
        const initial = list.find((p) => p.name === DEFAULT_CONDITION) ?? list[0];
        if (initial) {
          setSelected(initial.name);
          setParams(paramsToMs(initial.params));
        }
        setStatus("");
      })
      .catch((e: unknown) => {
        if (!live) return;
        setError(e instanceof Error ? e.message : String(e));
        setStatus("");
      });
    return () => {
      live = false;
    };
  }, []);

  // Overrides exist because a handler that also calls a setter cannot rely on
  // reading that state back: the closure captured here still holds the previous
  // value. Selecting a preset and re-running in the same tick is exactly that
  // case, and reading `selected` from the closure silently re-ran the old
  // condition while the controls showed the new one.
  const run = useCallback(
    async (override?: { params?: ParamsMs; custom?: boolean; condition?: string }) => {
      const effective = override?.params ?? params;
      if (!effective) return;
      const asCustom = override?.custom ?? custom;
      const condition = override?.condition ?? selected;

      setRunning(true);
      setError(null);

      // Let the button repaint before the synchronous wasm call blocks the thread.
      // setTimeout rather than requestAnimationFrame: rAF does not fire in a
      // hidden or throttled tab, so a run started in a background tab would wait
      // for a frame that never comes and the UI would sit on "running" forever.
      await new Promise((r) => setTimeout(r, 0));

      try {
        const result = await runSweep({
          condition: asCustom ? undefined : condition,
          params: asCustom ? effective : undefined,
          reps,
          count,
          periodMs: 20,
        });
        setPoints(result.points);
      } catch (e: unknown) {
        setError(e instanceof Error ? e.message : String(e));
      } finally {
        setRunning(false);
      }
    },
    [params, custom, selected, reps, count],
  );

  // Draw something as soon as the simulator is ready, rather than presenting an
  // empty chart and a button.
  useEffect(() => {
    if (params && points.length === 0 && !running && !error) {
      void run();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);

  const onSelectPreset = (name: string) => {
    const p = presets.find((x) => x.name === name);
    if (!p) return;
    setSelected(name);
    setCustom(false);
    const next = paramsToMs(p.params);
    setParams(next);
    void run({ params: next, custom: false, condition: name });
  };

  const onParamChange = (patch: Partial<ParamsMs>) => {
    setParams((prev) => (prev ? { ...prev, ...patch } : prev));
    setCustom(true);
  };

  return (
    <main className="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <header className="max-w-3xl">
        <h1 className="text-2xl font-semibold tracking-tight">jitter-bench</h1>
        <p className="mt-3 text-[15px] leading-relaxed text-neutral-700 dark:text-neutral-300">
          A jitter buffer trades latency for continuity and can do nothing else. Hold packets longer
          and fewer are missing when their turn to play arrives; every millisecond held is a
          millisecond added to the conversation. There is no setting that escapes the trade, so the
          only real question about a policy is where on the curve it lands.
        </p>
        <p className="mt-3 text-[13px] leading-relaxed text-neutral-600 dark:text-neutral-400">
          Everything below is computed in your browser by the Go simulator compiled to WebAssembly —
          the same code the command line tool runs, so this chart cannot disagree with the published
          tables. Conditions are synthetic and reproducible from a seed, not captures of real
          networks.
        </p>
      </header>

      {status && <p className="mt-8 text-[13px] text-neutral-500">{status}</p>}

      {error && (
        <div className="mt-8 rounded-lg border border-rose-300 bg-rose-50 p-4 text-[13px] text-rose-900 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200">
          <p className="font-semibold">Could not run the simulation.</p>
          <p className="mt-1 font-mono text-[12px]">{error}</p>
        </div>
      )}

      {params && (
        <div className="mt-8 grid gap-6 lg:grid-cols-[320px_minmax(0,1fr)]">
          <aside>
            <Controls
              presets={presets}
              selected={selected}
              params={params}
              reps={reps}
              count={count}
              running={running}
              onSelectPreset={onSelectPreset}
              onParamChange={onParamChange}
              onRepsChange={setReps}
              onCountChange={setCount}
              onRun={() => void run()}
            />
            {custom && (
              <p className="mt-2 text-[11px] text-amber-700 dark:text-amber-400">
                Running on custom parameters, not the <span className="font-mono">{selected}</span>{" "}
                preset.
              </p>
            )}
          </aside>

          <section className="space-y-5">
            <Verdict points={points} />
            <TradeoffChart points={points} />
            <ResultsTable points={points} />
          </section>
        </div>
      )}

      <footer className="mt-12 border-t border-neutral-200 pt-6 text-[12px] leading-relaxed text-neutral-500 dark:border-neutral-800">
        <p>
          The jitter estimate driving the adaptive policy is the one RFC 3550 §6.4.1 defines for RTCP
          receiver reports. It is structurally blind to sender clock drift: a drifting sender still
          stamps every frame one interval after the last, so the estimator sees a constant offset
          rather than variation. Set the drift slider away from zero and watch the adaptive policy
          fail to react.
        </p>
      </footer>
    </main>
  );
}
