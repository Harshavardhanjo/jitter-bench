package sim

import (
	"sort"
	"testing"
	"time"

	"github.com/Harshavardhanjo/jitter-bench/internal/buffer"
	"github.com/Harshavardhanjo/jitter-bench/internal/trace"
)

const period = 20 * time.Millisecond

func mustFixed(t *testing.T, d time.Duration) buffer.Policy {
	t.Helper()
	p, err := buffer.NewFixed(d)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// handTrace builds a trace directly so a test can state exactly what arrived
// when, which the generator's randomness cannot.
//
// It sorts by arrival because Trace.Packets is documented as arrival-ordered and
// Run scans it once, forwards. A helper that hands over unsorted packets makes
// Run silently skip them and produces failures that look like buffer bugs.
func handTrace(count int, pkts []trace.Packet) *trace.Trace {
	sorted := make([]trace.Packet, len(pkts))
	copy(sorted, pkts)

	// Fill in the RTP timestamp a real sender would have written, so the
	// estimator sees the same thing it sees in production. Tests that only care
	// about arrival timing need not restate it.
	for i := range sorted {
		if sorted[i].Stamp == 0 {
			sorted[i].Stamp = time.Duration(sorted[i].Seq) * period
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Arrived < sorted[j].Arrived })

	return &trace.Trace{
		Params:  trace.Params{Count: count, Period: period},
		Packets: sorted,
		Sent:    count,
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	if _, err := Run(nil, mustFixed(t, 0)); err == nil {
		t.Error("Run accepted a nil trace")
	}
	if _, err := Run(handTrace(1, nil), nil); err == nil {
		t.Error("Run accepted a nil policy")
	}
	if _, err := Run(handTrace(5, nil), mustFixed(t, 0)); err == nil {
		t.Error("Run accepted a trace with no packets")
	}
}

func TestPerfectPathPlaysEveryFrame(t *testing.T) {
	const n = 50
	pkts := make([]trace.Packet, 0, n)
	for i := 0; i < n; i++ {
		sent := time.Duration(i) * period
		pkts = append(pkts, trace.Packet{Seq: i, Sent: sent, Arrived: sent + 10*time.Millisecond})
	}

	res, err := Run(handTrace(n, pkts), mustFixed(t, 0))
	if err != nil {
		t.Fatal(err)
	}

	if res.Frames != n || res.Played != n {
		t.Errorf("Frames/Played = %d/%d, want %d/%d", res.Frames, res.Played, n, n)
	}
	if res.Underruns != 0 || res.LateDiscards != 0 {
		t.Errorf("underruns=%d lateDiscards=%d, want 0/0", res.Underruns, res.LateDiscards)
	}
	if got := time.Duration(res.PlayoutDelay.P50); got != 10*time.Millisecond {
		t.Errorf("median playout delay = %v, want the 10ms transit time", got)
	}
}

// The central trade, stated as a test: one packet arrives 30ms late. A buffer
// holding nothing discards it and plays a gap; a buffer holding 40ms plays it and
// charges everyone the latency.
func TestBufferDepthConvertsDiscardsIntoLatency(t *testing.T) {
	const n = 10
	build := func() []trace.Packet {
		pkts := make([]trace.Packet, 0, n)
		for i := 0; i < n; i++ {
			sent := time.Duration(i) * period
			extra := time.Duration(0)
			if i == 5 {
				extra = 30 * time.Millisecond
			}
			pkts = append(pkts, trace.Packet{Seq: i, Sent: sent, Arrived: sent + 5*time.Millisecond + extra})
		}
		return pkts
	}

	shallow, err := Run(handTrace(n, build()), mustFixed(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	if shallow.Underruns != 1 || shallow.LateDiscards != 1 {
		t.Errorf("shallow: underruns=%d lateDiscards=%d, want 1/1", shallow.Underruns, shallow.LateDiscards)
	}

	deep, err := Run(handTrace(n, build()), mustFixed(t, 40*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if deep.Underruns != 0 || deep.LateDiscards != 0 {
		t.Errorf("deep: underruns=%d lateDiscards=%d, want 0/0", deep.Underruns, deep.LateDiscards)
	}
	if deep.PlayoutDelay.P50 <= shallow.PlayoutDelay.P50 {
		t.Error("the deeper buffer did not cost more latency, so nothing was traded")
	}
}

func TestLostPacketsBecomeUnderrunsNotDiscards(t *testing.T) {
	const n = 10
	pkts := make([]trace.Packet, 0, n)
	for i := 0; i < n; i++ {
		if i == 4 || i == 5 || i == 6 {
			continue // never arrived
		}
		sent := time.Duration(i) * period
		pkts = append(pkts, trace.Packet{Seq: i, Sent: sent, Arrived: sent + 5*time.Millisecond})
	}

	res, err := Run(handTrace(n, pkts), mustFixed(t, 40*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if res.Underruns != 3 {
		t.Errorf("Underruns = %d, want 3", res.Underruns)
	}
	if res.LongestUnderrunRun != 3 {
		t.Errorf("LongestUnderrunRun = %d, want 3", res.LongestUnderrunRun)
	}
	if res.LateDiscards != 0 {
		t.Errorf("LateDiscards = %d, want 0; a lost packet was never late", res.LateDiscards)
	}
}

// Regression: a missing map key returns the zero Packet, whose Seq is 0, so
// checking the stored value instead of the key's presence misreports the very
// first packet of every stream as a duplicate.
func TestFirstPacketIsNotMistakenForADuplicate(t *testing.T) {
	pkts := []trace.Packet{
		{Seq: 0, Sent: 0, Arrived: 5 * time.Millisecond},
		{Seq: 1, Sent: period, Arrived: period + 5*time.Millisecond},
	}

	res, err := Run(handTrace(2, pkts), mustFixed(t, 40*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if res.DuplicatesDropped != 0 {
		t.Errorf("DuplicatesDropped = %d, want 0", res.DuplicatesDropped)
	}
	if res.Played != 2 {
		t.Errorf("Played = %d, want 2", res.Played)
	}
}

func TestRedundantCopiesAreDropped(t *testing.T) {
	pkts := []trace.Packet{
		{Seq: 0, Sent: 0, Arrived: 5 * time.Millisecond},
		{Seq: 0, Sent: 0, Arrived: 6 * time.Millisecond, Duplicate: true},
		{Seq: 1, Sent: period, Arrived: period + 5*time.Millisecond},
	}

	res, err := Run(handTrace(2, pkts), mustFixed(t, 40*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if res.DuplicatesDropped != 1 {
		t.Errorf("DuplicatesDropped = %d, want 1", res.DuplicatesDropped)
	}
	if res.Played != 2 || res.Underruns != 0 {
		t.Errorf("Played/Underruns = %d/%d, want 2/0", res.Played, res.Underruns)
	}
}

// Reordered packets that still beat their slot must be played. That is the
// primary thing a jitter buffer exists to do.
func TestReorderedButTimelyPacketsArePlayed(t *testing.T) {
	pkts := []trace.Packet{
		{Seq: 1, Sent: period, Arrived: 5 * time.Millisecond},
		{Seq: 0, Sent: 0, Arrived: 8 * time.Millisecond},
		{Seq: 2, Sent: 2 * period, Arrived: 2*period + 5*time.Millisecond},
	}

	res, err := Run(handTrace(3, pkts), mustFixed(t, 40*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if res.Underruns != 0 || res.LateDiscards != 0 {
		t.Errorf("underruns=%d lateDiscards=%d, want 0/0 for in-time reordering",
			res.Underruns, res.LateDiscards)
	}
}

func TestFixedPolicyNeverResyncs(t *testing.T) {
	tr, err := trace.Generate(trace.Params{
		Seed: 7, Count: 300, Period: period,
		BaseDelay: 20 * time.Millisecond, JitterStdDev: 15 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := Run(tr, mustFixed(t, 60*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if res.Resyncs != 0 {
		t.Errorf("Resyncs = %d for a fixed policy, want 0", res.Resyncs)
	}
	if got := time.Duration(res.Target.Max); got != 60*time.Millisecond {
		t.Errorf("target moved to %v under a fixed policy", got)
	}
}

func TestAdaptivePolicyRunsAndRespectsItsFloor(t *testing.T) {
	tr, err := trace.Generate(trace.Params{
		Seed: 11, Count: 600, Period: period,
		BaseDelay: 20 * time.Millisecond, JitterStdDev: 12 * time.Millisecond,
		SpikeProb: 0.03, SpikeDelay: 80 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	pol, err := buffer.NewAdaptive(buffer.DefaultAdaptiveConfig())
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(tr, pol)
	if err != nil {
		t.Fatal(err)
	}

	if res.Frames == 0 || res.Played == 0 {
		t.Fatalf("nothing played: %+v", res)
	}
	if res.Target.Min < float64(buffer.DefaultAdaptiveConfig().Min) {
		t.Error("target fell below the configured minimum")
	}
	if res.UnderrunRate < 0 || res.UnderrunRate > 1 {
		t.Errorf("UnderrunRate = %v, outside [0,1]", res.UnderrunRate)
	}
}

// Accounting must close: every frame slot either played or underran.
func TestFrameAccountingCloses(t *testing.T) {
	tr, err := trace.Generate(trace.Params{
		Seed: 3, Count: 400, Period: period,
		BaseDelay: 20 * time.Millisecond, JitterStdDev: 18 * time.Millisecond,
		LossProb: 0.02, DuplicateProb: 0.02,
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := Run(tr, mustFixed(t, 50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if res.Played+res.Underruns != res.Frames {
		t.Errorf("played %d + underruns %d != frames %d", res.Played, res.Underruns, res.Frames)
	}
	if res.PlayoutDelay.Count != res.Played {
		t.Errorf("delay samples %d != played frames %d", res.PlayoutDelay.Count, res.Played)
	}
}
