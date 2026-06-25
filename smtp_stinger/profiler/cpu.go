package profiler

import (
	"fmt"
	"os"
	"runtime/pprof"
)

func init() {
	Register("cpu", NewCPUProfile)
}

type CPUProfile struct {
	cfg  Config
	file *os.File
}

func NewCPUProfile(cfg Config) (Profile, error) {
	return &CPUProfile{
		cfg: cfg,
	}, nil
}

func (p *CPUProfile) Name() string {
	return "cpu"
}

func (p *CPUProfile) Start() error {
	f, err := os.Create(p.cfg.Path("cpu.prof"))
	if err != nil {
		return fmt.Errorf("create cpu profile: %w", err)
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("start cpu profile: %w", err)
	}

	p.file = f

	return nil
}

func (p *CPUProfile) Stop() error {
	pprof.StopCPUProfile()

	if p.file == nil {
		return nil
	}

	if err := p.file.Close(); err != nil {
		return fmt.Errorf("close cpu profile: %w", err)
	}

	p.file = nil

	return nil
}
