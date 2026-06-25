package profiler

import (
	"runtime"
	"time"
)

type statsCollector struct {
	interval time.Duration
	stopChan chan struct{}
	doneChan chan struct{}

	// Results
	goroutinesPeak int
	heapPeakAlloc  uint64
	initialGcRuns  uint32
	totalGcRuns    uint32
	gcPauseTotal   time.Duration
}

func newStatsCollector() *statsCollector {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &statsCollector{
		interval:      100 * time.Millisecond,
		stopChan:      make(chan struct{}),
		doneChan:      make(chan struct{}),
		initialGcRuns: m.NumGC,
	}
}

func (s *statsCollector) Start() {
	go func() {
		defer close(s.doneChan)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.collect()
			case <-s.stopChan:
				s.collect()
				return
			}
		}
	}()
}

func (s *statsCollector) Stop() {
	close(s.stopChan)
	<-s.doneChan
}

func (s *statsCollector) collect() {
	gCount := runtime.NumGoroutine()
	if gCount > s.goroutinesPeak {
		s.goroutinesPeak = gCount
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	if m.Alloc > s.heapPeakAlloc {
		s.heapPeakAlloc = m.Alloc
	}

	if m.NumGC >= s.initialGcRuns {
		s.totalGcRuns = m.NumGC - s.initialGcRuns
	}
	s.gcPauseTotal = time.Duration(m.PauseTotalNs)
}
