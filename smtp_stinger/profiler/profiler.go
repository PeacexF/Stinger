package profiler

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

type Profiler struct {
	cfg Config

	profiles []Profile

	startTime time.Time
	stopped   bool
	stats     *statsCollector
}

func Start() (*Profiler, error) {
	return StartWithConfig(ConfigFromEnv())
}

func StartWithConfig(cfg Config) (*Profiler, error) {
	p := &Profiler{
		cfg: cfg,
	}

	if !cfg.Enabled {
		return p, nil
	}

	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create profile directory: %w", err)
	}

	for _, name := range cfg.Profiles {
		factory, ok := registry[name]
		if !ok {
			return nil, fmt.Errorf("unknown profile: %q", name)
		}

		profile, err := factory(cfg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}

		p.profiles = append(p.profiles, profile)
	}

	p.startTime = time.Now()

	p.stats = newStatsCollector()
	p.stats.Start()

	for _, profile := range p.profiles {
		if err := profile.Start(); err != nil {
			_ = p.stopStarted()

			return nil, fmt.Errorf("%s: %w", profile.Name(), err)
		}
	}

	return p, nil
}

func (p *Profiler) Recover() {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "\n[Profiler] Caught panic: %v\n", r)
		fmt.Fprintln(os.Stderr, string(debug.Stack()))

		_ = p.Stop()

		panic(r)
	}
}

func (p *Profiler) Stop() error {
	if p == nil || !p.cfg.Enabled || p.stopped {
		return nil
	}

	p.stopped = true

	if p.stats != nil {
		p.stats.Stop()
	}

	var firstErr error

	for i := len(p.profiles) - 1; i >= 0; i-- {
		if err := p.profiles[i].Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	p.printSummary()

	return firstErr
}

func (p *Profiler) stopStarted() error {
	if p.stats != nil {
		p.stats.Stop()
	}
	var firstErr error

	for i := len(p.profiles) - 1; i >= 0; i-- {
		if err := p.profiles[i].Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (p *Profiler) Duration() time.Duration {
	if p.startTime.IsZero() {
		return 0
	}

	return time.Since(p.startTime)
}

func (p *Profiler) printSummary() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "==========================================")
	fmt.Fprintln(os.Stderr, "          SMTP-Stinger Profiler           ")
	fmt.Fprintln(os.Stderr, "==========================================")
	fmt.Fprintf(os.Stderr, "Elapsed:          %s\n", p.Duration().Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "CPUs / GOMAXPROC: %d / %d\n", runtime.NumCPU(), runtime.GOMAXPROCS(0))

	if p.stats != nil {
		fmt.Fprintf(os.Stderr, "Goroutines peak:  %d\n", p.stats.goroutinesPeak)
		fmt.Fprintln(os.Stderr, "\nHeap:")
		fmt.Fprintf(os.Stderr, "    Current:      %.2f MB\n", float64(m.Alloc)/1024/1024)
		fmt.Fprintf(os.Stderr, "    Peak:         %.2f MB\n", float64(p.stats.heapPeakAlloc)/1024/1024)

		fmt.Fprintln(os.Stderr, "\nGC:")
		fmt.Fprintf(os.Stderr, "    Runs:         %d\n", p.stats.totalGcRuns)
		fmt.Fprintf(os.Stderr, "    Pause total:  %s\n", p.stats.gcPauseTotal.Round(time.Millisecond))
	}

	fmt.Fprintln(os.Stderr, "\nProfiles saved to:")
	fmt.Fprintf(os.Stderr, "    %s/\n", p.cfg.OutputDir)
	for _, name := range p.cfg.Profiles {
		fmt.Fprintf(os.Stderr, "      - %s\n", name)
	}
	fmt.Fprintln(os.Stderr, "==========================================")
	fmt.Fprintln(os.Stderr)
}
