// Package sim plays a trace through a jitter buffer policy and measures what the
// listener would have heard.
//
// The model is a receiver that plays one frame every packetisation interval from
// a clock it does not adjust, which is what an audio device does. A frame is
// heard if its packet is in the buffer when its turn arrives; otherwise the
// receiver has nothing to play and the gap is counted. Packets that arrive after
// their turn has passed are discarded, because there is nowhere to put audio that
// should already have been heard.
//
// That last point is the one worth being explicit about: a late packet is not a
// small loss of quality, it is exactly as lost as one the network dropped, and
// the buffer chose to lose it. Holding packets longer converts those discards
// into latency. The measurement exists to price that conversion.
package sim

import (
	"fmt"
	"time"

	"github.com/Harshavardhanjo/cadence-bench/stats"

	"github.com/Harshavardhanjo/jitter-bench/internal/buffer"
	"github.com/Harshavardhanjo/jitter-bench/internal/trace"
)

// Result is what one policy did with one trace.
type Result struct {
	Policy string       `json:"policy"`
	Trace  trace.Params `json:"trace"`

	// Frames is how many frame slots the receiver tried to fill, counted from
	// the first packet it received to the end of the stream.
	Frames int `json:"frames"`

	// Played is how many of those slots had a packet available.
	Played int `json:"played"`

	// Underruns is how many had nothing, each one a gap the receiver has to
	// conceal. This is the continuity half of the trade.
	Underruns int `json:"underruns"`

	// LongestUnderrunRun is the most consecutive empty slots. It matters
	// separately from the count because concealment degrades badly with length:
	// scattered single-frame gaps are close to inaudible, and a run of ten is a
	// dropout anyone would notice.
	LongestUnderrunRun int `json:"longest_underrun_run"`

	// LateDiscards is how many packets arrived after their slot had passed.
	// These are the packets the buffer's own delay choice threw away.
	LateDiscards int `json:"late_discards"`

	// DuplicatesDropped is how many redundant copies were discarded, which is
	// correct behaviour rather than a fault.
	DuplicatesDropped int `json:"duplicates_dropped"`

	// Resyncs is how many times the policy changed its target enough to force
	// the playout schedule to shift. Each shift is an audible discontinuity in a
	// real receiver, which is why an adaptive policy that oscillates can sound
	// worse than a fixed one with more underruns.
	Resyncs int `json:"resyncs"`

	// UnderrunRate is Underruns over Frames.
	UnderrunRate float64 `json:"underrun_rate"`

	// PlayoutDelay is how long each played frame waited between being captured and
	// being heard, in nanoseconds, measured from the sender's real emission
	// instant rather than its RTP timestamp. This is the latency half of the trade
	// and the number a conversation actually feels.
	PlayoutDelay stats.Summary `json:"playout_delay_ns"`

	// Occupancy is how many packets were held at each slot boundary.
	Occupancy stats.Summary `json:"occupancy_packets"`

	// Target is the delay the policy asked for over the run, in nanoseconds.
	Target stats.Summary `json:"target_ns"`
}

// Run plays tr through p.
//
// Playout begins one target-delay after the first arrival, and the first frame
// played is the lowest sequence number held at that moment rather than the first
// packet to turn up. Those differ whenever the opening packets arrive reordered,
// and starting from the first arrival would invent a slot in the past for a
// packet that was comfortably in time.
//
// The initial target is read before any arrival is observed. An adaptive policy
// therefore opens at its configured floor and adjusts during the run, which is
// what a real receiver does: it cannot size itself against a stream it has not
// heard yet.
func Run(tr *trace.Trace, p buffer.Policy) (*Result, error) {
	if tr == nil {
		return nil, fmt.Errorf("sim: nil trace")
	}
	if p == nil {
		return nil, fmt.Errorf("sim: nil policy")
	}
	if len(tr.Packets) == 0 {
		return nil, fmt.Errorf("sim: trace delivered no packets, nothing to play")
	}

	period := tr.Params.Period
	res := &Result{Policy: p.Name(), Trace: tr.Params}

	held := make(map[int]trace.Packet)
	next := 0

	target := p.Target()
	playoutStart := tr.Packets[0].Arrived + target

	// Prebuffer: everything that lands before playout begins.
	for next < len(tr.Packets) && tr.Packets[next].Arrived <= playoutStart {
		pkt := tr.Packets[next]
		next++
		p.Observe(pkt.Stamp, pkt.Arrived)

		if _, dup := held[pkt.Seq]; dup {
			res.DuplicatesDropped++
		} else {
			held[pkt.Seq] = pkt
		}
	}

	startSeq := -1
	for seq := range held {
		if startSeq < 0 || seq < startSeq {
			startSeq = seq
		}
	}
	if startSeq < 0 {
		return nil, fmt.Errorf("sim: nothing buffered by playout start, which should be impossible")
	}

	playoutBase := playoutStart - time.Duration(startSeq)*period

	frames := tr.Params.Count - startSeq
	delays := make([]float64, 0, frames)
	occupancy := make([]float64, 0, frames)
	targets := make([]float64, 0, frames)

	run := 0
	for seq := startSeq; seq < tr.Params.Count; seq++ {
		slot := playoutBase + time.Duration(seq)*period

		// Everything that has arrived by now is available to this slot.
		for next < len(tr.Packets) && tr.Packets[next].Arrived <= slot {
			pkt := tr.Packets[next]
			next++
			p.Observe(pkt.Stamp, pkt.Arrived)

			// Testing whether a copy is already held, rather than trusting the
			// trace's duplicate flag, means a duplicate that overtakes its
			// original is simply used — which is what a real receiver does, since
			// it cannot know which copy the network sent first.
			if _, dup := held[pkt.Seq]; pkt.Seq < seq {
				// Its slot has already been and gone.
				res.LateDiscards++
			} else if dup {
				res.DuplicatesDropped++
			} else {
				held[pkt.Seq] = pkt
			}
		}

		res.Frames++
		targets = append(targets, float64(target))
		occupancy = append(occupancy, float64(len(held)))

		if pkt, ok := held[seq]; ok {
			res.Played++
			delays = append(delays, float64(slot-pkt.Sent))
			delete(held, seq)
			run = 0
		} else {
			res.Underruns++
			run++
			if run > res.LongestUnderrunRun {
				res.LongestUnderrunRun = run
			}
		}

		// Act on a target change only when it is worth at least half a frame.
		// Shifting the schedule is audible, so reacting to sub-frame wobble would
		// trade a real artefact for an imaginary improvement.
		if want := p.Target(); absDuration(want-target) >= period/2 {
			playoutBase += want - target
			target = want
			res.Resyncs++
		}
	}

	if res.Frames > 0 {
		res.UnderrunRate = float64(res.Underruns) / float64(res.Frames)
	}
	res.PlayoutDelay = stats.Summarize(delays)
	res.Occupancy = stats.Summarize(occupancy)
	res.Target = stats.Summarize(targets)

	return res, nil
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
