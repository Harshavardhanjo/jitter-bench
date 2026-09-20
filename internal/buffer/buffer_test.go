package buffer

import (
	"math"
	"testing"
	"time"
)

func TestNewFixedRejectsNegativeDelay(t *testing.T) {
	if _, err := NewFixed(-time.Millisecond); err == nil {
		t.Error("NewFixed accepted a negative delay")
	}
}

func TestFixedIsConstantAndIgnoresArrivals(t *testing.T) {
	f, err := NewFixed(60 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		f.Observe(time.Duration(i)*20*time.Millisecond, time.Duration(i)*97*time.Millisecond)
	}
	if got := f.Target(); got != 60*time.Millisecond {
		t.Errorf("Target = %v after wildly irregular arrivals, want 60ms", got)
	}
}

func TestNewAdaptiveValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AdaptiveConfig)
	}{
		{"zero multiplier", func(c *AdaptiveConfig) { c.Multiplier = 0 }},
		{"negative multiplier", func(c *AdaptiveConfig) { c.Multiplier = -1 }},
		{"negative min", func(c *AdaptiveConfig) { c.Min = -time.Millisecond }},
		{"max below min", func(c *AdaptiveConfig) { c.Max = c.Min - time.Millisecond }},
		{"negative shrinkAfter", func(c *AdaptiveConfig) { c.ShrinkAfter = -1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultAdaptiveConfig()
			c.mutate(&cfg)
			if _, err := NewAdaptive(cfg); err == nil {
				t.Fatalf("NewAdaptive accepted %s", c.name)
			}
		})
	}
}

// The estimator must be the one RFC 3550 section 6.4.1 specifies, worked through
// by hand, because the whole argument for using it is that the number matches
// what a real RTCP receiver report would carry.
func TestJitterEstimatorMatchesRFC3550(t *testing.T) {
	cfg := DefaultAdaptiveConfig()
	cfg.Min = 0 // do not let clamping hide the estimate
	a, err := NewAdaptive(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ms := func(f float64) time.Duration { return time.Duration(f * float64(time.Millisecond)) }

	// First arrival only seeds the previous-packet state; J stays zero.
	a.Observe(ms(0), ms(100))
	if got := a.Jitter(); got != 0 {
		t.Errorf("after one packet Jitter = %v, want 0", got)
	}

	// D = (130-100) - (20-0) = 10ms.  J = 0 + (10-0)/16 = 0.625ms
	a.Observe(ms(20), ms(130))
	if got, want := float64(a.Jitter()), float64(ms(0.625)); math.Abs(got-want) > float64(time.Microsecond) {
		t.Errorf("Jitter = %v, want 0.625ms", time.Duration(got))
	}

	// D = (150-130) - (40-20) = 0.  J = 0.625 + (0-0.625)/16 = 0.5859375ms
	a.Observe(ms(40), ms(150))
	if got, want := float64(a.Jitter()), float64(ms(0.5859375)); math.Abs(got-want) > float64(time.Microsecond) {
		t.Errorf("Jitter = %v, want 0.5859375ms", time.Duration(got))
	}
}

// D is a difference of differences, so a path that adds a large but constant
// delay must estimate the same jitter as one that adds none. If this fails the
// policy is sizing itself against latency instead of variation.
func TestJitterIsInsensitiveToConstantDelay(t *testing.T) {
	run := func(offset time.Duration) time.Duration {
		cfg := DefaultAdaptiveConfig()
		cfg.Min = 0
		a, _ := NewAdaptive(cfg)

		arrivals := []time.Duration{0, 7, 3, 11, 2, 9, 4}
		for i, extra := range arrivals {
			sent := time.Duration(i) * 20 * time.Millisecond
			a.Observe(sent, sent+offset+extra*time.Millisecond)
		}
		return a.Jitter()
	}

	near, far := run(0), run(2*time.Second)
	if near != far {
		t.Errorf("jitter estimate changed with constant delay: %v vs %v", near, far)
	}
}

func TestAdaptiveSettlesToMinimumOnAPerfectlyPacedPath(t *testing.T) {
	a, err := NewAdaptive(DefaultAdaptiveConfig())
	if err != nil {
		t.Fatal(err)
	}

	const period = 20 * time.Millisecond
	for i := 0; i < 1000; i++ {
		sent := time.Duration(i) * period
		a.Observe(sent, sent+30*time.Millisecond) // constant delay, zero variation
	}

	if got := a.Jitter(); got > time.Microsecond {
		t.Errorf("Jitter = %v on a perfectly paced path, want ~0", got)
	}
	if got := a.Target(); got != DefaultAdaptiveConfig().Min {
		t.Errorf("Target = %v, want the configured minimum %v", got, DefaultAdaptiveConfig().Min)
	}
}

// Growth is immediate and shrinking is not. The asymmetry is deliberate: a
// too-small buffer has already discarded packets by the time the estimate
// notices, while a too-large one only costs latency.
func TestAdaptiveGrowsImmediatelyAndShrinksSlowly(t *testing.T) {
	cfg := DefaultAdaptiveConfig()
	cfg.Min = 0
	cfg.ShrinkAfter = 10
	a, err := NewAdaptive(cfg)
	if err != nil {
		t.Fatal(err)
	}

	const period = 20 * time.Millisecond
	sent := time.Duration(0)
	arrive := time.Duration(0)

	// Seed, then deliver one badly late packet.
	a.Observe(sent, arrive)
	sent += period
	arrive += period + 160*time.Millisecond
	a.Observe(sent, arrive)

	grown := a.Target()
	if grown == 0 {
		t.Fatal("target did not grow after a large delay step")
	}

	// Now a perfectly paced run. The target must hold for at least ShrinkAfter
	// arrivals before it is allowed to fall.
	for i := 0; i < cfg.ShrinkAfter-1; i++ {
		sent += period
		arrive += period
		a.Observe(sent, arrive)
		if a.Target() < grown {
			t.Fatalf("target shrank after %d calm arrivals, want at least %d", i+1, cfg.ShrinkAfter)
		}
	}

	sent += period
	arrive += period
	a.Observe(sent, arrive)
	if a.Target() >= grown {
		t.Errorf("target did not shrink after %d calm arrivals", cfg.ShrinkAfter)
	}
}

func TestAdaptiveRespectsBounds(t *testing.T) {
	cfg := AdaptiveConfig{Multiplier: 3, Min: 40 * time.Millisecond, Max: 80 * time.Millisecond, ShrinkAfter: 1}
	a, err := NewAdaptive(cfg)
	if err != nil {
		t.Fatal(err)
	}

	const period = 20 * time.Millisecond
	sent, arrive := time.Duration(0), time.Duration(0)
	a.Observe(sent, arrive)

	// Hammer it with enormous variation; the target must not exceed Max.
	for i := 0; i < 200; i++ {
		sent += period
		arrive += period + time.Duration(i%2)*2*time.Second
		a.Observe(sent, arrive)
		if got := a.Target(); got > cfg.Max {
			t.Fatalf("Target = %v exceeds Max %v", got, cfg.Max)
		}
	}

	// And a calm run must not take it below Min.
	for i := 0; i < 2000; i++ {
		sent += period
		arrive += period
		a.Observe(sent, arrive)
		if got := a.Target(); got < cfg.Min {
			t.Fatalf("Target = %v is below Min %v", got, cfg.Min)
		}
	}
}

func TestNames(t *testing.T) {
	f, _ := NewFixed(60 * time.Millisecond)
	if got := f.Name(); got != "fixed-60ms" {
		t.Errorf("Fixed.Name = %q", got)
	}
	a, _ := NewAdaptive(DefaultAdaptiveConfig())
	if got := a.Name(); got != "adaptive" {
		t.Errorf("Adaptive.Name = %q", got)
	}
}

// The estimator is structurally blind to sender clock drift, and that is a
// property of RFC 3550 rather than of this implementation.
//
// A drifting sender still stamps every frame exactly one interval after the last,
// because it does not know its clock is wrong. So D is the same small constant on
// every packet rather than a growing quantity, and J — an average of |D| —
// converges to that constant and stops. The buffer therefore sits at its floor
// while the real required depth grows without bound.
//
// This is why the drift conditions in the sweep are not fixed by adaptation: no
// amount of tuning helps an estimator that cannot see the problem. Detecting
// drift needs a different signal, such as buffer occupancy trending in one
// direction over minutes.
func TestEstimatorIsBlindToClockDrift(t *testing.T) {
	cfg := DefaultAdaptiveConfig()
	a, err := NewAdaptive(cfg)
	if err != nil {
		t.Fatal(err)
	}

	const (
		period = 20 * time.Millisecond
		ppm    = 2000.0 // sender runs fast; exaggerated so the effect is quick
	)

	// Arrivals compress by the drift factor while RTP timestamps do not.
	for i := 0; i < 3000; i++ {
		stamp := time.Duration(i) * period
		arrived := time.Duration(float64(i)*float64(period)/(1+ppm/1e6)) + 20*time.Millisecond
		a.Observe(stamp, arrived)
	}

	// Drift of 2000ppm on a 20ms interval is 40us of offset per packet, so the
	// estimate should settle near that and nowhere near the tens of milliseconds
	// of real buffer the drift consumes over a minute.
	if got := a.Jitter(); got > 100*time.Microsecond {
		t.Errorf("Jitter = %v; expected it to settle near the 40us per-packet offset", got)
	}
	if got := a.Target(); got != cfg.Min {
		t.Errorf("Target = %v, want the floor %v: the estimator should not have reacted at all", got, cfg.Min)
	}
}
