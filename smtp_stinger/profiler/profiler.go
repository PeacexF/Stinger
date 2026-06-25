package profiler

import (
	"fmt"
	"os"
	"time"
)

type Profiler struct {
	cfg Config

	profiles []Profile

	startTime time.Time
	stopped   bool
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

	for _, profile := range p.profiles {
		if err := profile.Start(); err != nil {
			_ = p.stopStarted()

			return nil, fmt.Errorf("%s: %w", profile.Name(), err)
		}
	}

	return p, nil
}

func (p *Profiler) Stop() error {
	if p == nil || !p.cfg.Enabled || p.stopped {
		return nil
	}

	p.stopped = true

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
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "========== Profiler ==========")
	fmt.Fprintf(os.Stderr, "Duration: %s\n", p.Duration().Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "Profiles: %s\n", p.cfg.OutputDir)
	fmt.Fprintln(os.Stderr, "==============================")
	fmt.Fprintln(os.Stderr)
}
