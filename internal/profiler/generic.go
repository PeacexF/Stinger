package profiler

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
)

func init() {
	Register("heap", func(cfg Config) (Profile, error) { return NewGenericProfile(cfg, "heap", "heap.prof") })
	Register("allocs", func(cfg Config) (Profile, error) { return NewGenericProfile(cfg, "allocs", "allocs.prof") })
	Register("goroutine", func(cfg Config) (Profile, error) { return NewGenericProfile(cfg, "goroutine", "goroutine.prof") })
	Register("threadcreate", func(cfg Config) (Profile, error) { return NewGenericProfile(cfg, "threadcreate", "threadcreate.prof") })

	Register("mutex", func(cfg Config) (Profile, error) {
		runtime.SetMutexProfileFraction(5)
		return NewGenericProfile(cfg, "mutex", "mutex.prof")
	})
	Register("block", func(cfg Config) (Profile, error) {
		runtime.SetBlockProfileRate(1)
		return NewGenericProfile(cfg, "block", "block.prof")
	})
}

type GenericProfile struct {
	cfg      Config
	pName    string
	fileName string
}

func NewGenericProfile(cfg Config, pName, fileName string) (*GenericProfile, error) {
	return &GenericProfile{cfg: cfg, pName: pName, fileName: fileName}, nil
}

func (g *GenericProfile) Name() string { return g.pName }
func (g *GenericProfile) Start() error { return nil }

func (g *GenericProfile) Stop() error {
	f, err := os.Create(g.cfg.Path(g.fileName))
	if err != nil {
		return fmt.Errorf("create %s profile: %w", g.pName, err)
	}
	defer f.Close()

	if g.pName == "heap" {
		runtime.GC()
	}

	p := pprof.Lookup(g.pName)
	if p == nil {
		return fmt.Errorf("lookup profile %s failed", g.pName)
	}

	if err := p.WriteTo(f, 0); err != nil {
		return fmt.Errorf("write %s profile: %w", g.pName, err)
	}
	return nil
}
