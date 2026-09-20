// Mirrors the JSON the Go build emits. The field names are the Go struct tags,
// so if a tag changes the typecheck here does not catch it — that is the seam
// worth knowing about, and it is why the Go side spells every duration field with
// an Ns suffix rather than relying on anyone remembering the unit.

export type Spread = {
  median: number;
  min: number;
  max: number;
};

export type TraceParams = {
  seed: number;
  count: number;
  periodNs: number;
  baseDelayNs: number;
  jitterStdDevNs: number;
  spikeProb: number;
  spikeDelayNs: number;
  lossProb: number;
  burstEnterProb: number;
  burstLossProb: number;
  burstExitProb: number;
  duplicateProb: number;
  clockDriftPpm: number;
};

export type Point = {
  condition: string;
  policy: string;
  adaptive: boolean;
  reps: number;
  traceParams: TraceParams;
  medianPlayoutDelayNs: Spread;
  p99PlayoutDelayNs: Spread;
  underrunRate: Spread;
  longestUnderrunRun: Spread;
  lateDiscards: Spread;
  resyncs: Spread;
};

export type SweepResult = {
  periodNs: number;
  condition: string;
  description: string;
  points: Point[];
};

export type PresetInfo = {
  name: string;
  description: string;
  params: TraceParams;
};

/** Parameters the UI can set directly, in milliseconds where they are durations. */
export type ParamsMs = {
  jitterStdDevMs: number;
  baseDelayMs: number;
  spikeProb: number;
  spikeDelayMs: number;
  lossProb: number;
  burstEnterProb: number;
  burstLossProb: number;
  burstExitProb: number;
  duplicateProb: number;
  clockDriftPpm: number;
};

export type SweepRequest = {
  condition?: string;
  params?: ParamsMs;
  delaysMs?: number[];
  reps?: number;
  seed?: number;
  count?: number;
  periodMs?: number;
};

export const NS_PER_MS = 1e6;

export function msFromNs(ns: number): number {
  return ns / NS_PER_MS;
}

export function paramsToMs(p: TraceParams): ParamsMs {
  return {
    jitterStdDevMs: msFromNs(p.jitterStdDevNs),
    baseDelayMs: msFromNs(p.baseDelayNs),
    spikeProb: p.spikeProb,
    spikeDelayMs: msFromNs(p.spikeDelayNs),
    lossProb: p.lossProb,
    burstEnterProb: p.burstEnterProb,
    burstLossProb: p.burstLossProb,
    burstExitProb: p.burstExitProb,
    duplicateProb: p.duplicateProb,
    clockDriftPpm: p.clockDriftPpm,
  };
}
