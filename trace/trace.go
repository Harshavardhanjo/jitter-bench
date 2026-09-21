// Package trace generates deterministic synthetic network traces.
//
// A trace is the sequence of packet arrivals a receiver would observe for a
// stream sent at a fixed packetisation interval. It exists so that jitter buffer
// behaviour can be compared across runs and across machines: the same seed
// produces byte-identical arrivals, so a difference in the measured result is a
// difference in the buffer rather than in the weather.
//
// Reordering is not a parameter. It emerges, as it does in reality, from packets
// taking different amounts of time: give two consecutive packets delays that
// differ by more than the packetisation interval and they arrive swapped. A
// separate reorder knob would let a trace contain reordering without the delay
// variation that must accompany it, which is not a network any receiver will see.
//
// These are plausible synthetic conditions, not captures of real networks. The
// presets are labelled with the parameters that define them precisely so nobody
// reads "wifi" as a measurement of wifi.
package trace

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"
)

// Packet is one packet's fate in transit.
type Packet struct {
	// Seq is the sender's sequence number, starting at zero.
	Seq int `json:"seq"`

	// Stamp is the packet's RTP timestamp: the sender's own nominal clock, which
	// advances exactly one packetisation interval per packet by definition.
	// Drift never appears here, because a drifting sender does not know it is
	// drifting — it stamps every frame one interval after the last.
	//
	// This is the quantity RFC 3550's jitter estimator compares arrivals against,
	// so the estimator sees drift as a constant offset per packet rather than as
	// variation.
	Stamp time.Duration `json:"stampNs"`

	// Sent is when the sender actually emitted the packet, in receiver time.
	// Sender clock drift shows up here as a gradual stretch or compression of the
	// real send cadence, and it is the true capture instant, so end-to-end
	// latency is measured from this rather than from Stamp.
	Sent time.Duration `json:"sentNs"`

	// Arrived is when the receiver observed the packet.
	Arrived time.Duration `json:"arrivedNs"`

	// Duplicate marks a second copy of a packet the network delivered twice.
	// Both copies carry the same Seq and independent arrival times.
	Duplicate bool `json:"duplicate,omitempty"`
}

// Params defines a trace. Every field is deliberate: a trace generated from the
// same Params on any machine is identical.
type Params struct {
	// Seed makes generation reproducible.
	Seed int64 `json:"seed"`

	// Count is how many packets the sender emits.
	Count int `json:"count"`

	// Period is the packetisation interval. 20ms is one Opus frame at the usual
	// framing and the default throughout.
	Period time.Duration `json:"periodNs"`

	// BaseDelay is the fixed one-way transit time. It shifts every arrival
	// equally, so it has no effect on jitter buffer behaviour and exists only to
	// keep arrival times realistic.
	BaseDelay time.Duration `json:"baseDelayNs"`

	// JitterStdDev is the standard deviation of a normally distributed delay
	// added to BaseDelay. Total delay is clamped at zero: a packet cannot arrive
	// before it was sent.
	JitterStdDev time.Duration `json:"jitterStdDevNs"`

	// SpikeProb is the per-packet probability of an additional SpikeDelay.
	// Real networks produce occasional outliers far outside the normal spread —
	// a scheduling stall, a wifi retransmission, a handover — and a purely
	// gaussian trace understates how much buffer the tail demands.
	SpikeProb  float64       `json:"spikeProb"`
	SpikeDelay time.Duration `json:"spikeDelayNs"`

	// Loss follows a two-state Gilbert-Elliott model, because packet loss on
	// real paths clusters rather than arriving independently, and a buffer that
	// survives scattered single losses can still fail on a burst of the same
	// total count.
	//
	// LossProb applies while the path is in its good state. BurstEnterProb moves
	// it to the bad state, where BurstLossProb applies, and BurstExitProb
	// returns it to good. Setting BurstEnterProb to zero reduces this to
	// independent loss at LossProb.
	LossProb       float64 `json:"lossProb"`
	BurstEnterProb float64 `json:"burstEnterProb"`
	BurstLossProb  float64 `json:"burstLossProb"`
	BurstExitProb  float64 `json:"burstExitProb"`

	// DuplicateProb is the per-packet probability the network delivers a second
	// copy, which arrives with its own independent delay.
	DuplicateProb float64 `json:"duplicateProb"`

	// ClockDriftPPM is the sender's clock error in parts per million, positive
	// when the sender runs fast. Over a long call this is what separates sender
	// and receiver without bound, and it is the condition a fixed buffer cannot
	// survive however large it is.
	ClockDriftPPM float64 `json:"clockDriftPpm"`
}

// Validate reports whether the parameters describe a generatable trace.
func (p Params) Validate() error {
	if p.Count <= 0 {
		return fmt.Errorf("trace: count must be positive, got %d", p.Count)
	}
	if p.Period <= 0 {
		return fmt.Errorf("trace: period must be positive, got %v", p.Period)
	}
	if p.JitterStdDev < 0 {
		return fmt.Errorf("trace: jitter std dev must not be negative, got %v", p.JitterStdDev)
	}
	if p.SpikeDelay < 0 {
		return fmt.Errorf("trace: spike delay must not be negative, got %v", p.SpikeDelay)
	}
	probs := map[string]float64{
		"loss":        p.LossProb,
		"burst enter": p.BurstEnterProb,
		"burst loss":  p.BurstLossProb,
		"burst exit":  p.BurstExitProb,
		"duplicate":   p.DuplicateProb,
		"spike":       p.SpikeProb,
	}
	for name, v := range probs {
		if v < 0 || v > 1 {
			return fmt.Errorf("trace: %s probability must be in [0,1], got %v", name, v)
		}
	}
	return nil
}

// Trace is a generated set of arrivals, in the order the receiver sees them.
type Trace struct {
	Params Params

	// Packets is sorted by arrival time, which is the only order a receiver can
	// observe. Sequence numbers are therefore not monotonic here, and that is
	// the reordering the buffer has to cope with.
	Packets []Packet

	// Sent is how many packets the sender emitted, equal to Params.Count.
	Sent int

	// Lost is how many never arrived.
	Lost int

	// Duplicated is how many extra copies the network delivered.
	Duplicated int
}

// Generate builds a trace from p.
func Generate(p Params) (*Trace, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	rng := rand.New(rand.NewSource(p.Seed))

	// Sender period in receiver time. A sender running fast emits sooner than
	// the receiver expects, so positive drift shortens the interval.
	period := float64(p.Period) / (1 + p.ClockDriftPPM/1e6)

	out := &Trace{Params: p, Sent: p.Count, Packets: make([]Packet, 0, p.Count)}

	inBurst := false
	for seq := 0; seq < p.Count; seq++ {
		// Advance the loss state machine once per packet, before deciding this
		// packet's fate, so a burst can begin on any packet including the first.
		if inBurst {
			if rng.Float64() < p.BurstExitProb {
				inBurst = false
			}
		} else if rng.Float64() < p.BurstEnterProb {
			inBurst = true
		}

		lossProb := p.LossProb
		if inBurst {
			lossProb = p.BurstLossProb
		}

		sent := time.Duration(float64(seq) * period)

		if rng.Float64() < lossProb {
			out.Lost++
			continue
		}

		out.Packets = append(out.Packets, Packet{
			Seq:     seq,
			Stamp:   time.Duration(seq) * p.Period,
			Sent:    sent,
			Arrived: sent + delay(rng, p),
		})

		if rng.Float64() < p.DuplicateProb {
			out.Duplicated++
			out.Packets = append(out.Packets, Packet{
				Seq:       seq,
				Stamp:     time.Duration(seq) * p.Period,
				Sent:      sent,
				Arrived:   sent + delay(rng, p),
				Duplicate: true,
			})
		}
	}

	// Sort into arrival order. Ties break by sequence number so that generation
	// stays deterministic: sort.Slice is not stable, and two packets can share
	// an arrival time once durations are rounded to nanoseconds.
	sort.Slice(out.Packets, func(i, j int) bool {
		if out.Packets[i].Arrived != out.Packets[j].Arrived {
			return out.Packets[i].Arrived < out.Packets[j].Arrived
		}
		if out.Packets[i].Seq != out.Packets[j].Seq {
			return out.Packets[i].Seq < out.Packets[j].Seq
		}
		return !out.Packets[i].Duplicate && out.Packets[j].Duplicate
	})

	return out, nil
}

// delay draws one packet's transit time.
func delay(rng *rand.Rand, p Params) time.Duration {
	d := float64(p.BaseDelay) + rng.NormFloat64()*float64(p.JitterStdDev)
	if rng.Float64() < p.SpikeProb {
		d += float64(p.SpikeDelay)
	}
	return time.Duration(math.Max(0, d))
}
