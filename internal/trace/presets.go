package trace

import "time"

// Preset is a named parameter set.
//
// The names are shorthand for the parameters, not claims about the networks they
// are named after. Nothing here was captured from a real wifi link or a real
// cellular connection; each is a plausible set of numbers chosen to isolate a
// distinct failure mode, and Params is printed with every result so a reader can
// substitute their own.
type Preset struct {
	Name        string
	Description string
	Params      Params
}

// Presets returns the standard conditions, ordered from benign to hostile, with
// drift last because it is the odd one out: it is benign on every axis except
// the one that defeats a fixed buffer.
func Presets(seed int64, count int, period time.Duration) []Preset {
	base := Params{
		Seed:      seed,
		Count:     count,
		Period:    period,
		BaseDelay: 20 * time.Millisecond,
	}

	with := func(f func(*Params)) Params {
		p := base
		f(&p)
		return p
	}

	return []Preset{
		{
			Name:        "clean",
			Description: "wired path: 1ms jitter, no loss. The control condition.",
			Params: with(func(p *Params) {
				p.JitterStdDev = time.Millisecond
			}),
		},
		{
			Name:        "wifi",
			Description: "8ms jitter with 2% of packets delayed a further 60ms, light clustered loss.",
			Params: with(func(p *Params) {
				p.JitterStdDev = 8 * time.Millisecond
				p.SpikeProb = 0.02
				p.SpikeDelay = 60 * time.Millisecond
				p.LossProb = 0.002
				p.BurstEnterProb = 0.01
				p.BurstLossProb = 0.3
				p.BurstExitProb = 0.4
			}),
		},
		{
			Name:        "congested",
			Description: "20ms jitter, 5% of packets delayed a further 150ms, heavy clustered loss.",
			Params: with(func(p *Params) {
				p.JitterStdDev = 20 * time.Millisecond
				p.SpikeProb = 0.05
				p.SpikeDelay = 150 * time.Millisecond
				p.LossProb = 0.005
				p.BurstEnterProb = 0.03
				p.BurstLossProb = 0.5
				p.BurstExitProb = 0.25
			}),
		},
		{
			Name:        "mobile",
			Description: "15ms jitter, rare 200ms stalls, clustered loss and occasional duplicates.",
			Params: with(func(p *Params) {
				p.JitterStdDev = 15 * time.Millisecond
				p.SpikeProb = 0.03
				p.SpikeDelay = 200 * time.Millisecond
				p.LossProb = 0.003
				p.BurstEnterProb = 0.02
				p.BurstLossProb = 0.45
				p.BurstExitProb = 0.2
				p.DuplicateProb = 0.005
			}),
		},
		{
			Name:        "drift",
			Description: "1ms jitter, no loss, but the sender's clock runs 200ppm fast.",
			Params: with(func(p *Params) {
				p.JitterStdDev = time.Millisecond
				p.ClockDriftPPM = 200
			}),
		},
	}
}

// PresetByName returns the named preset.
func PresetByName(name string, seed int64, count int, period time.Duration) (Preset, bool) {
	for _, p := range Presets(seed, count, period) {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}
