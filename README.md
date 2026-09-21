# jitter-bench

A jitter buffer trades latency for continuity and can do nothing else. Hold
packets longer and fewer are missing when their turn to play arrives; every
millisecond held is a millisecond added to the conversation. No setting escapes
the trade, so the only real question about a policy is where on the curve it
lands — and whether it stays there when conditions change.

```
go build ./cmd/jitterbench
./jitterbench presets      # the synthetic network conditions
./jitterbench sweep        # trace the curve, place the adaptive policy on it
```

There is also a **browser UI** at
**[jitter-bench.harshavardhanjo.com](https://jitter-bench.harshavardhanjo.com)**,
running the same simulator compiled to WebAssembly, so its chart cannot disagree
with these tables. Source in [ui/](ui).

```
cmd/jitterbench ──┐
ui (wasm) ────────┴── internal/report   sweep + aggregate across repetitions
                          ├── trace             deterministic network traces
                          ├── buffer            fixed and adaptive delay policies
                          ├── internal/sim      playout, and what the listener lost
                          └── cadence-bench/stats   shared percentile definitions
```

This is the receive side of the audio path. Its sibling
[cadence-bench](https://github.com/Harshavardhanjo/cadence-bench) measures the
send side — whether a loop can hold a 20ms deadline at all — and the two share a
statistics package so their percentiles mean the same thing.

`trace` and `buffer` are importable packages rather than internal ones, so other
tools can generate the same network conditions and run the same playout policies:

```
go get github.com/Harshavardhanjo/jitter-bench@v0.1.0
```

## What it measures

**Playout delay** is the latency half: how long each frame waited between being
captured and being heard. **Underruns** are the continuity half: frame slots
where the buffer had nothing to play.

Three secondary figures carry most of the insight:

- **late discards** — packets that arrived after their slot and were thrown away.
  These are the buffer's own doing. A late packet is exactly as lost as one the
  network dropped, and the difference is that the buffer chose it.
- **longest underrun run** — concealment degrades badly with length. Scattered
  single-frame gaps are near-inaudible; a run of ten is a dropout anyone notices.
- **resyncs** — how often an adaptive policy shifted its playout schedule. Each
  shift is audible in a real receiver, and neither the delay nor the underrun
  column counts it.

## Results

20ms packetisation, 3000 packets (one minute of audio), 5 repetitions per point,
each a different network draw from the same distribution. Figures are the median
across repetitions with the observed range in brackets. Reproduce with
`./jitterbench sweep -reps 5 -count 3000`.

### wifi — 8ms jitter, 2% of packets delayed a further 60ms, light clustered loss

| policy | median delay | underruns | late | resyncs |
|---|---|---|---|---|
| fixed-20ms | 39.6ms | 3.57% (3.23–4.83) | 85 | 0 |
| fixed-40ms | 59.6ms | 2.73% (2.63–3.27) | 61 | 0 |
| fixed-60ms | 79.6ms | 1.93% (1.70–2.17) | 32 | 0 |
| fixed-80ms | 99.6ms | 0.73% (0.70–1.07) | 0 | 0 |
| fixed-260ms | 279.6ms | 0.73% (0.70–1.07) | 0 | 0 |
| **adaptive** | **60.2ms** | **2.40% (2.40–3.17)** | 51 | **62** |

Two things to read here. Past 80ms the curve goes flat: every remaining underrun
is a packet the network actually lost, and no amount of buffer invents it back.
And adaptive lands on the useful part of the curve — 2.40% at 60ms, where the
cheapest fixed delay that is at least as continuous costs 80ms — but it pays 62
resyncs to get there.

### mobile — 15ms jitter, rare 200ms stalls, clustered loss and duplicates

| policy | median delay | underruns | late | resyncs |
|---|---|---|---|---|
| fixed-20ms | 37.3ms | 17.97% (14.23–52.92) | 405 | 0 |
| fixed-40ms | 59.2ms | 8.03% (6.30–8.53) | 97 | 0 |
| **fixed-60ms** | **79.2ms** | **7.30% (6.00–8.03)** | 80 | 0 |
| fixed-120ms | 139.2ms | 7.30% (6.00–8.03) | 80 | 0 |
| fixed-260ms | 279.2ms | 4.57% (3.47–5.20) | 0 | 0 |
| adaptive | 131.7ms | 7.23% (6.70–8.00) | 82 | 105 |

**Here the adaptive policy loses outright.** A fixed 60ms delay is as continuous
as adaptive — 7.30% against 7.23% — for 52ms less latency and no resyncs. The
rare 200ms stalls drag the jitter estimate up, adaptive grows to accommodate
them, and because they are rare the extra depth buys almost nothing. A smoothed
mean deviation is the wrong statistic for a distribution whose damage lives
entirely in its tail.

### congested — 20ms jitter, 5% of packets delayed a further 150ms, heavy clustered loss

| policy | median delay | underruns | late | resyncs |
|---|---|---|---|---|
| fixed-20ms | 36.4ms | 28.80% (20.90–55.15) | 656 | 0 |
| fixed-60ms | 78.9ms | 11.27% (10.20–12.03) | 139 | 0 |
| fixed-180ms | 198.9ms | 7.03% (5.30–7.40) | 11 | 0 |
| fixed-260ms | 278.9ms | 6.80% (4.83–6.93) | 0 | 0 |
| adaptive | 152.6ms | 9.70% (8.70–10.57) | 106 | 116 |

Note the range on `fixed-20ms`: 20.90% to 55.15% across five draws of the same
distribution. A single run here would have reported anything in that span with
equal confidence, which is the argument for repetitions in one line.

### drift — a clean path and a wrong clock

At 2000ppm, openly exaggerated so the effect fits in a one-minute trace. Real
clocks are 10–100ppm, where the same failure takes tens of minutes.

| policy | drift-fast: median delay | drift-slow: underruns |
|---|---|---|
| fixed-20ms | 99.8ms (p99 158.5ms) | 83.33% |
| fixed-60ms | 139.8ms | 50.13% |
| fixed-120ms | 199.8ms | 0.50% |
| fixed-260ms | 339.8ms | 0.00% |
| adaptive | 99.8ms | 83.33% |

**The adaptive policy is exactly as bad as the fixed one it is built on, because
it never notices.** Its estimator is the one RFC 3550 §6.4.1 defines, and that
estimator is structurally blind to clock drift: a drifting sender still stamps
every frame one packetisation interval after the last, since it does not know its
clock is wrong. So `D` is the same small constant on every packet rather than a
growing quantity, `J` converges to that constant and stops, and the buffer sits at
its floor while the required depth walks away from it. A test pins this directly.

The two directions fail differently. A fast sender fills the buffer and grows
latency without ever underrunning — 99.8ms median against 39.9ms on a clean path,
still climbing at the end of the trace. A slow sender drains it and then underruns
every frame. Depth only decides how long the slow case takes to arrive: at this
drift rate 120ms survives a minute and 20ms does not, and given ten minutes none
of them do.

Detecting drift needs a different signal — buffer occupancy trending in one
direction over minutes — and that is not implemented here.

## Methodology

- **Deterministic traces.** Same seed, identical arrivals. A result that cannot be
  regenerated is an anecdote.
- **Repetitions vary the draw, not the seed.** Rerunning one trace would only
  confirm the simulation is deterministic, which a test already does.
- **Error bars are observed min and max**, not a standard error. Underrun rates
  are bounded at zero and driven by rare bursts, so an interval assuming normality
  would understate the tail.
- **Percentiles are nearest-rank**, shared with `cadence-bench` so the two
  repositories' numbers are comparable.
- **Reordering is not a parameter.** It emerges from packets taking different
  amounts of time, as it does on a real path. A reorder knob would permit traces
  with reordering but without the delay variation that must accompany it.
- **Loss is a two-state Gilbert–Elliott process**, because loss clusters, and a
  buffer that survives scattered single losses can still fail on a burst of the
  same total count.
- **The estimator is fed RTP timestamps**, not the sender's true emission instant.
  Getting this wrong cancels the drift term out, and would have made the blindness
  finding an artefact of the model rather than a property of the RFC.

## What this does not measure

- **Audio quality.** Nothing is encoded, decoded or played. A missed deadline is
  reported as a missed deadline, not as an audible artefact; how bad a given miss
  rate sounds depends on the concealment algorithm, which is a different project.
- **Concealment or FEC.** No packet loss concealment, no in-band FEC, no
  retransmission. Every underrun is counted as a total loss, which overstates the
  damage a real decoder would suffer.
- **Real networks.** Every condition is synthetic. The parameters are printed with
  every result precisely so nobody reads "wifi" as a measurement of wifi.
- **Adaptive policies other than this one.** One RFC 3550 estimator with growth
  and shrink asymmetry. It is the standard approach, not the best known one, and
  the mobile result above is a reason to want better.
- **Multi-stream or CPU effects.** One stream, no contention, no scheduler. The
  send-side version of those questions is what `cadence-bench` measures.
- **Receiver clock drift.** Only the sender's clock is wrong here.

## Running the UI

```
cd ui
npm install
npm run dev
```

Deployment is a local build followed by `vercel deploy --prod` from `ui/out`,
not a git-connected build: the build shells out to the Go toolchain to produce
the wasm and the hosting build image has no Go, so a push-triggered build fails
on its first step.

`npm run dev` rebuilds the wasm binary first, so it can never be stale against the
Go source it came from. `npm run build` produces a static export with no server:
the simulation runs in the browser, which is also the honest architecture for a
benchmark, since a hosted backend would make the numbers depend on a machine the
reader cannot inspect.

## Tests

```
go test ./...
go test -race ./...
```

The race detector needs a C toolchain, which on Windows means installing one; CI
runs it on Linux, Windows and macOS regardless.

## License

MIT. See [LICENSE](LICENSE).
