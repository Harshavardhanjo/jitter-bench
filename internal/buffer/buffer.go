// Package buffer implements jitter buffer delay policies.
//
// A jitter buffer trades latency for continuity and can do nothing else. Holding
// packets longer means fewer of them are missing when their turn to play
// arrives, and every millisecond held is a millisecond added to the
// conversation's round trip. There is no setting that avoids the trade, so the
// only meaningful question about a policy is where on that curve it lands and how
// well it stays there when conditions change.
//
// Two policies are implemented. A fixed delay is the baseline and the thing to
// beat: sweeping it traces the curve itself. An adaptive delay estimates jitter
// from arrivals and sizes itself, which should let it sit near the curve without
// being told the conditions in advance.
package buffer

import (
	"fmt"
	"time"
)

// Policy decides how long the receiver holds packets before playing them.
//
// Observe is called for every arriving packet in arrival order. Target is read at
// each frame boundary, so a policy may change its answer over time; the cost of
// acting on a change is charged by the simulation, not here.
type Policy interface {
	// Observe records an arrival. sent and arrived are both on the receiver's
	// timeline, relative to the start of the stream.
	Observe(sent, arrived time.Duration)

	// Target reports the delay the receiver should currently hold.
	Target() time.Duration

	// Name identifies the policy for reporting.
	Name() string
}

// Fixed holds every packet for the same delay.
//
// It cannot adapt, which makes it both the baseline and a demonstration: too
// small and it discards late packets it already received, too large and it adds
// latency nobody needed, and against a drifting sender clock no value works
// because the required delay grows without bound.
type Fixed struct {
	delay time.Duration
}

// NewFixed returns a fixed-delay policy.
func NewFixed(delay time.Duration) (*Fixed, error) {
	if delay < 0 {
		return nil, fmt.Errorf("buffer: fixed delay must not be negative, got %v", delay)
	}
	return &Fixed{delay: delay}, nil
}

func (f *Fixed) Observe(sent, arrived time.Duration) {}
func (f *Fixed) Target() time.Duration               { return f.delay }
func (f *Fixed) Name() string                        { return fmt.Sprintf("fixed-%v", f.delay) }

// AdaptiveConfig configures an Adaptive policy.
type AdaptiveConfig struct {
	// Multiplier scales the jitter estimate into a delay target. The estimate is
	// a smoothed mean absolute deviation, not a tail bound, so a multiplier
	// above one is what buys headroom for the outliers that actually cause
	// underruns. Three is the default.
	Multiplier float64

	// Min and Max bound the target. Min keeps the buffer from collapsing to zero
	// on a briefly perfect path; Max keeps a pathological estimate from adding
	// unbounded latency.
	Min, Max time.Duration

	// ShrinkAfter is how many consecutive arrivals must agree that a smaller
	// target is enough before the policy reduces it. Growth is immediate.
	//
	// The asymmetry is the point. Being too small costs discarded packets
	// immediately, and being too large costs only latency, so the risks are not
	// symmetric and the response should not be either. Shrinking eagerly also
	// makes the buffer oscillate: it shrinks into the next delay spike, underruns,
	// grows again, and each resize costs an audible discontinuity.
	ShrinkAfter int
}

// DefaultAdaptiveConfig returns settings suitable for a 20ms packetisation
// interval.
func DefaultAdaptiveConfig() AdaptiveConfig {
	return AdaptiveConfig{
		Multiplier:  3,
		Min:         20 * time.Millisecond,
		Max:         400 * time.Millisecond,
		ShrinkAfter: 50,
	}
}

// Adaptive sizes itself from an interarrival jitter estimate.
//
// The estimate is the one defined in RFC 3550 section 6.4.1 for RTCP receiver
// reports:
//
//	D(i-1,i) = (R(i) - R(i-1)) - (S(i) - S(i-1))
//	J(i)     = J(i-1) + (|D(i-1,i)| - J(i-1)) / 16
//
// D is how much later a packet arrived than its send spacing implies, so it is
// zero on a perfectly paced path regardless of how much fixed delay that path
// adds. J is an exponentially weighted mean of its magnitude, with the 1/16 gain
// the RFC specifies. Using the standard estimator rather than inventing one means
// the number here is the same number an RTCP receiver report would carry, which
// is what makes it checkable against a real stack.
type Adaptive struct {
	cfg AdaptiveConfig

	jitter float64 // J, in nanoseconds
	target time.Duration

	havePrev    bool
	prevSent    time.Duration
	prevArrived time.Duration

	// shrinkVotes counts consecutive arrivals whose estimate would allow a
	// smaller target.
	shrinkVotes int
}

// NewAdaptive returns an adaptive policy.
func NewAdaptive(cfg AdaptiveConfig) (*Adaptive, error) {
	if cfg.Multiplier <= 0 {
		return nil, fmt.Errorf("buffer: multiplier must be positive, got %v", cfg.Multiplier)
	}
	if cfg.Min < 0 {
		return nil, fmt.Errorf("buffer: min must not be negative, got %v", cfg.Min)
	}
	if cfg.Max < cfg.Min {
		return nil, fmt.Errorf("buffer: max %v is below min %v", cfg.Max, cfg.Min)
	}
	if cfg.ShrinkAfter < 0 {
		return nil, fmt.Errorf("buffer: shrinkAfter must not be negative, got %d", cfg.ShrinkAfter)
	}
	return &Adaptive{cfg: cfg, target: cfg.Min}, nil
}

// Observe updates the jitter estimate from one arrival.
func (a *Adaptive) Observe(sent, arrived time.Duration) {
	if !a.havePrev {
		a.havePrev = true
		a.prevSent, a.prevArrived = sent, arrived
		return
	}

	d := float64((arrived - a.prevArrived) - (sent - a.prevSent))
	if d < 0 {
		d = -d
	}
	a.jitter += (d - a.jitter) / 16

	a.prevSent, a.prevArrived = sent, arrived

	want := time.Duration(a.cfg.Multiplier * a.jitter)
	if want < a.cfg.Min {
		want = a.cfg.Min
	}
	if want > a.cfg.Max {
		want = a.cfg.Max
	}

	switch {
	case want > a.target:
		// Grow at once: the packets a too-small buffer discards are already lost
		// by the time the estimate notices.
		a.target = want
		a.shrinkVotes = 0
	case want < a.target:
		a.shrinkVotes++
		if a.shrinkVotes >= a.cfg.ShrinkAfter {
			a.target = want
			a.shrinkVotes = 0
		}
	default:
		a.shrinkVotes = 0
	}
}

func (a *Adaptive) Target() time.Duration { return a.target }
func (a *Adaptive) Name() string          { return "adaptive" }

// Jitter reports the current RFC 3550 estimate, for reporting and tests.
func (a *Adaptive) Jitter() time.Duration { return time.Duration(a.jitter) }
