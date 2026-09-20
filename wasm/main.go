//go:build js && wasm

// Command wasm exposes the sweep to JavaScript.
//
// The browser UI calls into this rather than reimplementing the simulation, so the
// curve it draws is produced by the same code as the command line tool and the
// published tables. A TypeScript port would be faster to write and would drift
// from the Go the first time either changed, which for a measurement tool is the
// one failure that matters: the chart and the benchmark would disagree and both
// would look authoritative.
package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"
	"time"

	"github.com/Harshavardhanjo/jitter-bench/internal/report"
	"github.com/Harshavardhanjo/jitter-bench/internal/trace"
)

// request is what the UI sends. Durations cross the boundary as milliseconds
// because that is what a JSON number and an HTML slider both handle naturally;
// they are converted to time.Duration here, once.
type request struct {
	// Condition names a preset. Ignored when Params is supplied.
	Condition string `json:"condition"`

	// Params overrides the preset entirely, for the UI's sliders.
	Params *paramsMs `json:"params"`

	DelaysMs []float64 `json:"delaysMs"`
	Reps     int       `json:"reps"`
	Seed     int64     `json:"seed"`
	Count    int       `json:"count"`
	PeriodMs float64   `json:"periodMs"`
}

// paramsMs mirrors trace.Params with durations in milliseconds.
type paramsMs struct {
	JitterStdDevMs float64 `json:"jitterStdDevMs"`
	BaseDelayMs    float64 `json:"baseDelayMs"`
	SpikeProb      float64 `json:"spikeProb"`
	SpikeDelayMs   float64 `json:"spikeDelayMs"`
	LossProb       float64 `json:"lossProb"`
	BurstEnterProb float64 `json:"burstEnterProb"`
	BurstLossProb  float64 `json:"burstLossProb"`
	BurstExitProb  float64 `json:"burstExitProb"`
	DuplicateProb  float64 `json:"duplicateProb"`
	ClockDriftPPM  float64 `json:"clockDriftPpm"`
}

func ms(v float64) time.Duration { return time.Duration(v * float64(time.Millisecond)) }

func (p paramsMs) toTrace(seed int64, count int, period time.Duration) trace.Params {
	return trace.Params{
		Seed:           seed,
		Count:          count,
		Period:         period,
		BaseDelay:      ms(p.BaseDelayMs),
		JitterStdDev:   ms(p.JitterStdDevMs),
		SpikeProb:      p.SpikeProb,
		SpikeDelay:     ms(p.SpikeDelayMs),
		LossProb:       p.LossProb,
		BurstEnterProb: p.BurstEnterProb,
		BurstLossProb:  p.BurstLossProb,
		BurstExitProb:  p.BurstExitProb,
		DuplicateProb:  p.DuplicateProb,
		ClockDriftPPM:  p.ClockDriftPPM,
	}
}

func fail(err error) string {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}

func ok(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fail(err)
	}
	return string(b)
}

// presets returns the named conditions with their parameters, so the UI can show
// what each one actually is rather than only its label.
func presets(this js.Value, args []js.Value) any {
	type out struct {
		Name        string       `json:"name"`
		Description string       `json:"description"`
		Params      trace.Params `json:"params"`
	}

	list := trace.Presets(1, 3000, 20*time.Millisecond)
	res := make([]out, 0, len(list))
	for _, p := range list {
		res = append(res, out{p.Name, p.Description, p.Params})
	}
	return ok(res)
}

// sweep runs the measurement. It is synchronous and can take a second or two for
// a large request, so the UI calls it from a worker.
func sweep(this js.Value, args []js.Value) any {
	if len(args) != 1 || args[0].Type() != js.TypeString {
		return fail(fmt.Errorf("sweep expects one JSON string argument"))
	}

	var req request
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		return fail(fmt.Errorf("bad request: %w", err))
	}

	if req.Reps <= 0 {
		req.Reps = 3
	}
	if req.Count <= 0 {
		req.Count = 1500
	}
	if req.PeriodMs <= 0 {
		req.PeriodMs = 20
	}
	if req.Seed == 0 {
		req.Seed = 1
	}
	period := ms(req.PeriodMs)

	if len(req.DelaysMs) == 0 {
		req.DelaysMs = []float64{20, 40, 60, 80, 120, 180, 260}
	}
	delays := make([]time.Duration, 0, len(req.DelaysMs))
	for _, d := range req.DelaysMs {
		if d < 0 {
			return fail(fmt.Errorf("negative delay %vms", d))
		}
		delays = append(delays, ms(d))
	}

	var preset trace.Preset
	switch {
	case req.Params != nil:
		preset = trace.Preset{
			Name:        "custom",
			Description: "parameters set in the UI",
			Params:      req.Params.toTrace(req.Seed, req.Count, period),
		}
	default:
		p, found := trace.PresetByName(req.Condition, req.Seed, req.Count, period)
		if !found {
			return fail(fmt.Errorf("unknown condition %q", req.Condition))
		}
		preset = p
	}

	if err := preset.Params.Validate(); err != nil {
		return fail(err)
	}

	points, err := report.Sweep(preset, delays, req.Reps)
	if err != nil {
		return fail(err)
	}

	return ok(struct {
		PeriodNs    int64          `json:"periodNs"`
		Condition   string         `json:"condition"`
		Description string         `json:"description"`
		Points      []report.Point `json:"points"`
	}{period.Nanoseconds(), preset.Name, preset.Description, points})
}

func main() {
	js.Global().Set("jitterbench", js.ValueOf(map[string]any{
		"presets": js.FuncOf(presets),
		"sweep":   js.FuncOf(sweep),
	}))

	// Keep the module alive so the exported functions stay callable. A wasm main
	// that returns takes its exports with it.
	select {}
}
