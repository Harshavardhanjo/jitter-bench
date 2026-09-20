package report

import (
	"testing"
	"time"

	"github.com/Harshavardhanjo/jitter-bench/internal/buffer"
	"github.com/Harshavardhanjo/jitter-bench/internal/trace"
)

func params() trace.Params {
	return trace.Params{
		Seed:         1,
		Count:        400,
		Period:       20 * time.Millisecond,
		BaseDelay:    20 * time.Millisecond,
		JitterStdDev: 10 * time.Millisecond,
	}
}

func TestParseDelays(t *testing.T) {
	got, err := ParseDelays("20ms, 60ms,120ms")
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{20 * time.Millisecond, 60 * time.Millisecond, 120 * time.Millisecond}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("delay %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestParseDelaysRejectsBadInput(t *testing.T) {
	for _, spec := range []string{"", "   ", "banana", "-20ms", "20"} {
		if _, err := ParseDelays(spec); err == nil {
			t.Errorf("ParseDelays(%q) succeeded, want error", spec)
		}
	}
}

func TestMeasureRejectsNonPositiveReps(t *testing.T) {
	for _, reps := range []int{0, -1} {
		_, err := Measure("x", params(), reps, func() (buffer.Policy, error) {
			return buffer.NewFixed(60 * time.Millisecond)
		})
		if err == nil {
			t.Errorf("Measure(reps=%d) succeeded, want error", reps)
		}
	}
}

// Each repetition must be a different network draw. If they were identical the
// spread would collapse to zero and the error bars would be decorative.
func TestRepetitionsVaryTheDraw(t *testing.T) {
	p := params()
	p.LossProb = 0.03
	p.SpikeProb = 0.05
	p.SpikeDelay = 120 * time.Millisecond

	pt, err := Measure("x", p, 5, func() (buffer.Policy, error) {
		return buffer.NewFixed(40 * time.Millisecond)
	})
	if err != nil {
		t.Fatal(err)
	}
	if pt.Reps != 5 {
		t.Errorf("Reps = %d, want 5", pt.Reps)
	}
	if pt.UnderrunRate.Min == pt.UnderrunRate.Max {
		t.Error("every repetition produced the same underrun rate; the draws are not varying")
	}
	if pt.UnderrunRate.Min > pt.UnderrunRate.Median || pt.UnderrunRate.Median > pt.UnderrunRate.Max {
		t.Errorf("spread is not ordered: %+v", pt.UnderrunRate)
	}
}

func TestMeasureFlagsTheAdaptivePolicy(t *testing.T) {
	fixed, err := Measure("x", params(), 1, func() (buffer.Policy, error) {
		return buffer.NewFixed(60 * time.Millisecond)
	})
	if err != nil {
		t.Fatal(err)
	}
	if fixed.Adaptive {
		t.Error("a fixed policy was flagged adaptive")
	}

	adaptive, err := Measure("x", params(), 1, func() (buffer.Policy, error) {
		return buffer.NewAdaptive(buffer.DefaultAdaptiveConfig())
	})
	if err != nil {
		t.Fatal(err)
	}
	if !adaptive.Adaptive {
		t.Error("the adaptive policy was not flagged; the UI uses this to pick it out")
	}
}

func TestSweepCoversEveryDelayPlusAdaptive(t *testing.T) {
	delays := []time.Duration{20 * time.Millisecond, 60 * time.Millisecond}
	pr := trace.Preset{Name: "x", Description: "test", Params: params()}

	pts, err := Sweep(pr, delays, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != len(delays)+1 {
		t.Fatalf("got %d points, want %d fixed plus adaptive", len(pts), len(delays)+1)
	}

	adaptives := 0
	for _, p := range pts {
		if p.Condition != "x" {
			t.Errorf("point carries condition %q", p.Condition)
		}
		if p.Adaptive {
			adaptives++
		}
	}
	if adaptives != 1 {
		t.Errorf("found %d adaptive points, want exactly 1", adaptives)
	}
}
