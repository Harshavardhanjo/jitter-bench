// Command jitterbench measures the latency a jitter buffer must add to keep
// audio continuous.
//
// A jitter buffer trades latency for continuity and can do nothing else. Sweeping
// a fixed delay traces that trade as a curve; an adaptive policy has to find a
// point on it without being told the conditions in advance. The question the tool
// answers is whether it does.
//
//	jitterbench presets          list the network conditions
//	jitterbench sweep            trace the curve and place adaptive on it
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/Harshavardhanjo/jitter-bench/internal/report"
	"github.com/Harshavardhanjo/jitter-bench/internal/trace"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "presets":
		err = presetsCmd(os.Args[2:])
	case "sweep":
		err = sweepCmd(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "jitterbench: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "jitterbench: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `jitterbench measures the latency a jitter buffer must add to keep audio continuous.

usage:
  jitterbench presets [flags]   list the synthetic network conditions
  jitterbench sweep   [flags]   trace the latency/continuity curve

Run a command with -h for its flags.
`)
}

func durNS(ns float64) string {
	switch a := math.Abs(ns); {
	case a >= 1e6:
		return fmt.Sprintf("%.1fms", ns/1e6)
	case a >= 1e3:
		return fmt.Sprintf("%.0fus", ns/1e3)
	default:
		return fmt.Sprintf("%.0fns", ns)
	}
}

func presetsCmd(args []string) error {
	fs := flag.NewFlagSet("presets", flag.ExitOnError)
	seed := fs.Int64("seed", 1, "seed the parameters are reported for")
	count := fs.Int("count", 3000, "packets per trace")
	period := fs.Duration("period", 20*time.Millisecond, "packetisation interval")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Println("synthetic conditions. plausible parameters, not captures of real networks.")
	fmt.Println()
	for _, pr := range trace.Presets(*seed, *count, *period) {
		p := pr.Params
		fmt.Printf("%-10s %s\n", pr.Name, pr.Description)
		fmt.Printf("%-10s   jitter sd %-8v spike %.0f%% of %-8v loss %.1f%%  burst enter %.0f%%/loss %.0f%%/exit %.0f%%  dup %.1f%%  drift %.0fppm\n\n",
			"", p.JitterStdDev, p.SpikeProb*100, p.SpikeDelay, p.LossProb*100,
			p.BurstEnterProb*100, p.BurstLossProb*100, p.BurstExitProb*100,
			p.DuplicateProb*100, p.ClockDriftPPM)
	}
	return nil
}

func sweepCmd(args []string) error {
	fs := flag.NewFlagSet("sweep", flag.ExitOnError)
	conditions := fs.String("conditions", "all", "comma-separated preset names, or all")
	delaySpec := fs.String("delays", "20ms,40ms,60ms,80ms,120ms,180ms,260ms", "fixed delays to sweep")
	seed := fs.Int64("seed", 1, "base seed; repetition i uses seed+i")
	count := fs.Int("count", 3000, "packets per trace (3000 x 20ms is one minute of audio)")
	period := fs.Duration("period", 20*time.Millisecond, "packetisation interval")
	reps := fs.Int("reps", 5, "repetitions per point; the spread across them is the error bar")
	out := fs.String("out", "", "also write the full JSON report here")
	if err := fs.Parse(args); err != nil {
		return err
	}

	delays, err := report.ParseDelays(*delaySpec)
	if err != nil {
		return err
	}

	all := trace.Presets(*seed, *count, *period)
	var chosen []trace.Preset
	if *conditions == "all" {
		chosen = all
	} else {
		for _, name := range strings.Split(*conditions, ",") {
			name = strings.TrimSpace(name)
			pr, ok := trace.PresetByName(name, *seed, *count, *period)
			if !ok {
				names := make([]string, 0, len(all))
				for _, p := range all {
					names = append(names, p.Name)
				}
				return fmt.Errorf("unknown condition %q; known: %s", name, strings.Join(names, ", "))
			}
			chosen = append(chosen, pr)
		}
	}

	rep := &report.Report{PeriodNs: period.Nanoseconds()}
	for _, pr := range chosen {
		fmt.Fprintf(os.Stderr, "measuring %s (%d reps x %d packets)\n", pr.Name, *reps, *count)

		pts, err := report.Sweep(pr, delays, *reps)
		if err != nil {
			return err
		}
		rep.Points = append(rep.Points, pts...)
	}

	printTable(rep, chosen)

	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()

		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\nwrote %s\n", *out)
	}
	return nil
}

func printTable(rep *report.Report, presets []trace.Preset) {
	fmt.Printf("\nperiod %v. median of per-run figures, range across runs in brackets.\n",
		time.Duration(rep.PeriodNs))

	for _, pr := range presets {
		fmt.Printf("\n%s - %s\n\n", pr.Name, pr.Description)
		fmt.Printf("  %-12s %14s %14s %18s %8s %8s %8s\n",
			"policy", "median delay", "p99 delay", "underruns", "worst", "late", "resync")
		fmt.Printf("  %-12s %14s %14s %18s %8s %8s %8s\n",
			strings.Repeat("-", 12), strings.Repeat("-", 14), strings.Repeat("-", 14),
			strings.Repeat("-", 18), strings.Repeat("-", 8), strings.Repeat("-", 8), strings.Repeat("-", 8))

		for _, pt := range rep.Points {
			if pt.Condition != pr.Name {
				continue
			}
			under := fmt.Sprintf("%.2f%% (%.2f-%.2f)",
				pt.UnderrunRate.Median*100, pt.UnderrunRate.Min*100, pt.UnderrunRate.Max*100)
			fmt.Printf("  %-12s %14s %14s %18s %8.0f %8.0f %8.0f\n",
				pt.Policy,
				durNS(pt.MedianDelay.Median),
				durNS(pt.P99Delay.Median),
				under,
				pt.LongestRun.Median,
				pt.LateDiscards.Median,
				pt.Resyncs.Median)
		}
	}

	fmt.Printf("\nunderruns = frame slots with nothing to play. worst = longest consecutive run.\n")
	fmt.Printf("late = packets that arrived after their slot, discarded by the buffer's own delay choice.\n")
	fmt.Printf("resync = playout schedule shifts, each one audible in a real receiver.\n")
}
