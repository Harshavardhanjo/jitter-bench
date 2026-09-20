package trace

import (
	"testing"
	"time"
)

func testParams() Params {
	return Params{
		Seed:         1,
		Count:        500,
		Period:       20 * time.Millisecond,
		BaseDelay:    20 * time.Millisecond,
		JitterStdDev: 5 * time.Millisecond,
	}
}

func TestValidateRejectsBadParams(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Params)
	}{
		{"zero count", func(p *Params) { p.Count = 0 }},
		{"zero period", func(p *Params) { p.Period = 0 }},
		{"negative jitter", func(p *Params) { p.JitterStdDev = -time.Millisecond }},
		{"negative spike delay", func(p *Params) { p.SpikeDelay = -time.Millisecond }},
		{"loss above one", func(p *Params) { p.LossProb = 1.5 }},
		{"negative duplicate prob", func(p *Params) { p.DuplicateProb = -0.1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := testParams()
			c.mutate(&p)
			if _, err := Generate(p); err == nil {
				t.Fatalf("Generate accepted %s", c.name)
			}
		})
	}
}

// Reproducibility is the whole premise: a result that cannot be regenerated is
// an anecdote.
func TestGenerateIsDeterministic(t *testing.T) {
	a, err := Generate(testParams())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(testParams())
	if err != nil {
		t.Fatal(err)
	}

	if len(a.Packets) != len(b.Packets) {
		t.Fatalf("packet counts differ: %d vs %d", len(a.Packets), len(b.Packets))
	}
	for i := range a.Packets {
		if a.Packets[i] != b.Packets[i] {
			t.Fatalf("packet %d differs: %+v vs %+v", i, a.Packets[i], b.Packets[i])
		}
	}
	if a.Lost != b.Lost || a.Duplicated != b.Duplicated {
		t.Errorf("counts differ: lost %d/%d duplicated %d/%d", a.Lost, b.Lost, a.Duplicated, b.Duplicated)
	}
}

func TestDifferentSeedsDiffer(t *testing.T) {
	p := testParams()
	a, _ := Generate(p)
	p.Seed = 2
	b, _ := Generate(p)

	same := len(a.Packets) == len(b.Packets)
	if same {
		for i := range a.Packets {
			if a.Packets[i] != b.Packets[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Error("two seeds produced identical traces")
	}
}

func TestPacketsAreInArrivalOrder(t *testing.T) {
	tr, err := Generate(testParams())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(tr.Packets); i++ {
		if tr.Packets[i].Arrived < tr.Packets[i-1].Arrived {
			t.Fatalf("packet %d arrived before its predecessor", i)
		}
	}
}

// A receiver cannot be handed a packet before the sender emitted it, whatever
// the delay distribution draws.
func TestNoPacketArrivesBeforeItWasSent(t *testing.T) {
	p := testParams()
	p.JitterStdDev = 50 * time.Millisecond // wide enough to draw large negatives
	p.BaseDelay = time.Millisecond

	tr, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, pk := range tr.Packets {
		if pk.Arrived < pk.Sent {
			t.Fatalf("seq %d arrived %v before it was sent at %v", pk.Seq, pk.Sent-pk.Arrived, pk.Sent)
		}
	}
}

func TestAccountingIsConsistent(t *testing.T) {
	p := testParams()
	p.LossProb = 0.05
	p.DuplicateProb = 0.05

	tr, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := tr.Sent - tr.Lost + tr.Duplicated; len(tr.Packets) != want {
		t.Errorf("have %d packets, want sent-lost+duplicated = %d", len(tr.Packets), want)
	}
	if tr.Sent != p.Count {
		t.Errorf("Sent = %d, want %d", tr.Sent, p.Count)
	}
}

// Reordering is deliberately not a parameter; it has to emerge from delay
// variation. If this stops holding, the generator is no longer producing the
// condition a jitter buffer exists to handle.
func TestReorderingEmergesFromDelayVariation(t *testing.T) {
	p := testParams()
	p.JitterStdDev = 30 * time.Millisecond // comfortably above the 20ms period

	tr, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}

	swaps := 0
	for i := 1; i < len(tr.Packets); i++ {
		if tr.Packets[i].Seq < tr.Packets[i-1].Seq {
			swaps++
		}
	}
	if swaps == 0 {
		t.Error("no reordering with jitter above the packetisation interval")
	}

	// And the control: jitter well below the period must not reorder.
	p.JitterStdDev = 200 * time.Microsecond
	calm, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(calm.Packets); i++ {
		if calm.Packets[i].Seq < calm.Packets[i-1].Seq {
			t.Errorf("reordering at %v jitter with a %v period", p.JitterStdDev, p.Period)
			break
		}
	}
}

// Burst loss must actually cluster, or the Gilbert-Elliott state machine is not
// doing anything that independent loss would not.
func TestBurstLossClusters(t *testing.T) {
	p := testParams()
	p.BurstEnterProb = 0.05
	p.BurstLossProb = 1.0
	p.BurstExitProb = 0.1

	tr, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}

	present := make(map[int]bool, len(tr.Packets))
	for _, pk := range tr.Packets {
		present[pk.Seq] = true
	}
	longest, current := 0, 0
	for seq := 0; seq < tr.Sent; seq++ {
		if present[seq] {
			current = 0
			continue
		}
		current++
		if current > longest {
			longest = current
		}
	}
	if longest < 3 {
		t.Errorf("longest loss run was %d; a burst model should produce runs", longest)
	}
}

func TestPositiveClockDriftShortensTheSendCadence(t *testing.T) {
	p := testParams()
	p.JitterStdDev = 0
	p.ClockDriftPPM = 10000 // 1%, exaggerated so the effect is unambiguous

	tr, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}

	last := tr.Packets[len(tr.Packets)-1]
	nominal := time.Duration(last.Seq) * p.Period
	if last.Sent >= nominal {
		t.Errorf("with a fast sender clock, packet %d was sent at %v, expected earlier than %v",
			last.Seq, last.Sent, nominal)
	}
}

func TestPresetsAreDistinctAndOrdered(t *testing.T) {
	got := Presets(1, 100, 20*time.Millisecond)
	if len(got) == 0 {
		t.Fatal("no presets")
	}

	seen := map[string]bool{}
	for _, pr := range got {
		if seen[pr.Name] {
			t.Errorf("duplicate preset %q", pr.Name)
		}
		seen[pr.Name] = true
		if pr.Description == "" {
			t.Errorf("preset %q has no description", pr.Name)
		}
		if err := pr.Params.Validate(); err != nil {
			t.Errorf("preset %q is invalid: %v", pr.Name, err)
		}
	}
	if got[0].Name != "clean" {
		t.Errorf("first preset is %q, want the benign control first", got[0].Name)
	}
}

func TestPresetByName(t *testing.T) {
	if _, ok := PresetByName("wifi", 1, 100, 20*time.Millisecond); !ok {
		t.Error("wifi preset not found")
	}
	if _, ok := PresetByName("nonexistent", 1, 100, 20*time.Millisecond); ok {
		t.Error("PresetByName invented a preset")
	}
}
