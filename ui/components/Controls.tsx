"use client";

import type { ParamsMs, PresetInfo } from "@/lib/types";

type Props = {
  presets: PresetInfo[];
  selected: string;
  params: ParamsMs;
  reps: number;
  count: number;
  running: boolean;
  onSelectPreset: (name: string) => void;
  onParamChange: (patch: Partial<ParamsMs>) => void;
  onRepsChange: (n: number) => void;
  onCountChange: (n: number) => void;
  onRun: () => void;
};

type SliderProps = {
  label: string;
  hint?: string;
  value: number;
  min: number;
  max: number;
  step: number;
  format: (v: number) => string;
  onChange: (v: number) => void;
};

function Slider({ label, hint, value, min, max, step, format, onChange }: SliderProps) {
  return (
    <label className="block">
      <span className="flex items-baseline justify-between gap-2">
        <span className="text-[13px] font-medium text-neutral-800 dark:text-neutral-200">{label}</span>
        <span className="font-mono text-[12px] tabular-nums text-neutral-600 dark:text-neutral-400">
          {format(value)}
        </span>
      </span>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="mt-1 w-full accent-sky-600"
      />
      {hint && <span className="mt-0.5 block text-[11px] leading-snug text-neutral-500">{hint}</span>}
    </label>
  );
}

const pct = (v: number) => `${(v * 100).toFixed(1)}%`;
const msFmt = (v: number) => `${v.toFixed(0)}ms`;

export function Controls(props: Props) {
  const { presets, selected, params, reps, count, running } = props;
  const preset = presets.find((p) => p.name === selected);

  return (
    <div className="space-y-5 rounded-lg border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-950">
      <div>
        <label className="text-[13px] font-medium text-neutral-800 dark:text-neutral-200">
          Network condition
        </label>
        <select
          value={selected}
          onChange={(e) => props.onSelectPreset(e.target.value)}
          className="mt-1 w-full rounded border border-neutral-300 bg-white px-2 py-1.5 text-[13px] dark:border-neutral-700 dark:bg-neutral-900"
        >
          {presets.map((p) => (
            <option key={p.name} value={p.name}>
              {p.name}
            </option>
          ))}
        </select>
        {preset && (
          <p className="mt-1.5 text-[11px] leading-snug text-neutral-500">{preset.description}</p>
        )}
        <p className="mt-1 text-[11px] leading-snug text-neutral-500">
          Moving any slider detaches from the preset; the sweep then runs on exactly what is shown.
        </p>
      </div>

      <div className="space-y-4">
        <Slider
          label="Jitter (std dev)"
          hint="Delay variation. Above one packet interval it starts reordering packets."
          value={params.jitterStdDevMs}
          min={0}
          max={60}
          step={1}
          format={msFmt}
          onChange={(v) => props.onParamChange({ jitterStdDevMs: v })}
        />
        <Slider
          label="Spike size"
          hint="Extra delay on an outlier. The tail, not the average, is what empties a buffer."
          value={params.spikeDelayMs}
          min={0}
          max={300}
          step={10}
          format={msFmt}
          onChange={(v) => props.onParamChange({ spikeDelayMs: v })}
        />
        <Slider
          label="Spike frequency"
          value={params.spikeProb}
          min={0}
          max={0.15}
          step={0.005}
          format={pct}
          onChange={(v) => props.onParamChange({ spikeProb: v })}
        />
        <Slider
          label="Burst loss entry"
          hint="Chance of dropping into a lossy state. Loss on real paths clusters."
          value={params.burstEnterProb}
          min={0}
          max={0.1}
          step={0.005}
          format={pct}
          onChange={(v) => props.onParamChange({ burstEnterProb: v })}
        />
        <Slider
          label="Clock drift"
          hint="Sender clock error. No depth fixes it: positive grows latency, negative starves."
          value={params.clockDriftPpm}
          min={-3000}
          max={3000}
          step={100}
          format={(v) => `${v.toFixed(0)} ppm`}
          onChange={(v) => props.onParamChange({ clockDriftPpm: v })}
        />
      </div>

      <div className="grid grid-cols-2 gap-3 border-t border-neutral-200 pt-4 dark:border-neutral-800">
        <Slider
          label="Repetitions"
          hint="Different network draws. Their spread is the error bar."
          value={reps}
          min={1}
          max={9}
          step={1}
          format={(v) => v.toFixed(0)}
          onChange={props.onRepsChange}
        />
        <Slider
          label="Packets"
          hint="At 20ms each, 1500 is 30 seconds of audio."
          value={count}
          min={300}
          max={4500}
          step={300}
          format={(v) => v.toFixed(0)}
          onChange={props.onCountChange}
        />
      </div>

      <button
        onClick={props.onRun}
        disabled={running}
        className="w-full rounded bg-neutral-900 px-3 py-2 text-[13px] font-medium text-white transition hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
      >
        {running ? "Running in WebAssembly…" : "Run sweep"}
      </button>
    </div>
  );
}
