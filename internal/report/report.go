// Package report runs a sweep and aggregates it.
//
// It exists so that the command line tool and the WebAssembly build used by the
// browser UI share one implementation. A UI that re-derived these numbers in
// JavaScript could disagree with the published table, and the two would drift
// apart at exactly the moment someone checked. Everything the UI plots comes
// through this package, compiled from the same source as the binary.
package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/Harshavardhanjo/cadence-bench/stats"

	"github.com/Harshavardhanjo/jitter-bench/internal/buffer"
	"github.com/Harshavardhanjo/jitter-bench/internal/sim"
	"github.com/Harshavardhanjo/jitter-bench/internal/trace"
)

// Spread is the range of one figure across repetitions, reported as the observed
// median with min and max rather than a standard error. Underrun rates are
// bounded at zero and driven by rare bursts, so an interval computed as though
// they were normally distributed would understate the tail.
type Spread struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

func spreadOf(v []float64) Spread {
	if len(v) == 0 {
		return Spread{}
	}
	s := stats.Summarize(v)
	return Spread{Median: s.P50, Min: s.Min, Max: s.Max}
}

// Point is one policy measured against one condition, across repetitions.
type Point struct {
	Condition    string       `json:"condition"`
	Policy       string       `json:"policy"`
	Adaptive     bool         `json:"adaptive"`
	Reps         int          `json:"reps"`
	Params       trace.Params `json:"traceParams"`
	MedianDelay  Spread       `json:"medianPlayoutDelayNs"`
	P99Delay     Spread       `json:"p99PlayoutDelayNs"`
	UnderrunRate Spread       `json:"underrunRate"`
	LongestRun   Spread       `json:"longestUnderrunRun"`
	LateDiscards Spread       `json:"lateDiscards"`
	Resyncs      Spread       `json:"resyncs"`
}

// Report is a complete sweep.
type Report struct {
	PeriodNs int64   `json:"periodNs"`
	Points   []Point `json:"points"`
}

// Measure runs one policy against one condition across reps repetitions.
//
// Each repetition is a different network draw from the same distribution rather
// than a rerun of the same trace. Repeating an identical trace would only confirm
// that the simulation is deterministic, which a test already does; varying the
// draw is what makes the spread an error bar.
func Measure(condition string, base trace.Params, reps int, newPolicy func() (buffer.Policy, error)) (Point, error) {
	if reps <= 0 {
		return Point{}, fmt.Errorf("report: reps must be positive, got %d", reps)
	}

	var (
		med, p99, under, longest, late, resync []float64
		pol                                    buffer.Policy
	)

	for i := 0; i < reps; i++ {
		p := base
		p.Seed = base.Seed + int64(i)

		tr, err := trace.Generate(p)
		if err != nil {
			return Point{}, err
		}
		pol, err = newPolicy()
		if err != nil {
			return Point{}, err
		}
		res, err := sim.Run(tr, pol)
		if err != nil {
			return Point{}, err
		}

		med = append(med, res.PlayoutDelay.P50)
		p99 = append(p99, res.PlayoutDelay.P99)
		under = append(under, res.UnderrunRate)
		longest = append(longest, float64(res.LongestUnderrunRun))
		late = append(late, float64(res.LateDiscards))
		resync = append(resync, float64(res.Resyncs))
	}

	_, isAdaptive := pol.(*buffer.Adaptive)

	return Point{
		Condition:    condition,
		Policy:       pol.Name(),
		Adaptive:     isAdaptive,
		Reps:         reps,
		Params:       base,
		MedianDelay:  spreadOf(med),
		P99Delay:     spreadOf(p99),
		UnderrunRate: spreadOf(under),
		LongestRun:   spreadOf(longest),
		LateDiscards: spreadOf(late),
		Resyncs:      spreadOf(resync),
	}, nil
}

// Sweep measures every fixed delay plus the adaptive policy against one condition.
func Sweep(pr trace.Preset, delays []time.Duration, reps int) ([]Point, error) {
	out := make([]Point, 0, len(delays)+1)

	for _, d := range delays {
		delay := d
		pt, err := Measure(pr.Name, pr.Params, reps, func() (buffer.Policy, error) {
			return buffer.NewFixed(delay)
		})
		if err != nil {
			return nil, err
		}
		out = append(out, pt)
	}

	pt, err := Measure(pr.Name, pr.Params, reps, func() (buffer.Policy, error) {
		return buffer.NewAdaptive(buffer.DefaultAdaptiveConfig())
	})
	if err != nil {
		return nil, err
	}
	return append(out, pt), nil
}

// ParseDelays reads a comma-separated list of durations.
func ParseDelays(spec string) ([]time.Duration, error) {
	var out []time.Duration
	for _, f := range strings.Split(spec, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		d, err := time.ParseDuration(f)
		if err != nil {
			return nil, fmt.Errorf("bad delay %q: %w", f, err)
		}
		if d < 0 {
			return nil, fmt.Errorf("delay %v is negative", d)
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no fixed delays given")
	}
	return out, nil
}
