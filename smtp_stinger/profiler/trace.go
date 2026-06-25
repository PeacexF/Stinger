package profiler

import (
	"fmt"
	"os"
	"runtime/trace"
)

func init() {
	Register("trace", NewTraceProfile)
}

type TraceProfile struct {
	cfg  Config
	file *os.File
}

func NewTraceProfile(cfg Config) (Profile, error) {
	return &TraceProfile{cfg: cfg}, nil
}

func (p *TraceProfile) Name() string { return "trace" }

func (p *TraceProfile) Start() error {
	f, err := os.Create(p.cfg.Path("trace.out"))
	if err != nil {
		return fmt.Errorf("create trace file: %w", err)
	}

	if err := trace.Start(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("start trace: %w", err)
	}

	p.file = f
	return nil
}

func (p *TraceProfile) Stop() error {
	trace.Stop()
	if p.file == nil {
		return nil
	}
	if err := p.file.Close(); err != nil {
		return fmt.Errorf("close trace file: %w", err)
	}
	p.file = nil
	return nil
}
