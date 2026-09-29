// Package output streams results to valid_emails.txt / results.jsonl and
// summarises a finished results.jsonl.
package output

import (
	"bufio"
	"encoding/json"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/PeacexF/Stinger/internal/verify"
)

// Marker receives every written email (e.g. a checkpoint)
type Marker interface{ Mark(email string) }

// Writer appends results to the valid and jsonl files. Safe for concurrent use.
type Writer struct {
	ValidPath string
	JSONLPath string

	mu          sync.Mutex
	vf, jf      *os.File
	counters    map[verify.Status]int
	subCounters map[verify.SubStatus]int
	marker      Marker
}

// Open creates outputDir and opens both files, appending or truncating
func Open(outputDir, validName, jsonlName string, appendMode bool, marker Marker) (*Writer, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	w := &Writer{
		ValidPath:   filepath.Join(outputDir, validName),
		JSONLPath:   filepath.Join(outputDir, jsonlName),
		counters:    make(map[verify.Status]int),
		subCounters: make(map[verify.SubStatus]int),
		marker:      marker,
	}
	var err error
	if w.vf, err = os.OpenFile(w.ValidPath, flags, 0o644); err != nil {
		return nil, err
	}
	if w.jf, err = os.OpenFile(w.JSONLPath, flags, 0o644); err != nil {
		w.vf.Close()
		return nil, err
	}
	return w, nil
}

// Write records one result. Each line is written straight to the file so
// nothing is lost if the process is killed.
func (w *Writer) Write(r verify.Result) error {
	line, err := r.JSON()
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.jf.Write(append(line, '\n')); err != nil {
		return err
	}
	if r.Status == verify.Valid || r.Status == verify.CatchAll {
		if _, err := w.vf.WriteString(r.Email + "\n"); err != nil {
			return err
		}
	}
	w.counters[r.Status]++
	if r.SubStatus != nil {
		w.subCounters[*r.SubStatus]++
	}
	if w.marker != nil {
		w.marker.Mark(r.Email)
	}
	return nil
}

// Counters returns a copy of the per-status counts
func (w *Writer) Counters() map[verify.Status]int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return maps.Clone(w.counters)
}

// SubCounters returns a copy of the per-sub-status counts
func (w *Writer) SubCounters() map[verify.SubStatus]int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return maps.Clone(w.subCounters)
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	err1 := w.vf.Close()
	err2 := w.jf.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

type Summary struct {
	Total           int
	Counts          map[string]int
	SubCounts       map[string]int
	AvgDurationMS   float64
	CatchAllDomains []string
	SampleErrors    []string
}

// Summarise reads results.jsonl, skipping blank and corrupt lines
func Summarise(path string) (*Summary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &Summary{Counts: map[string]int{}, SubCounts: map[string]int{}, SampleErrors: []string{}}
	catchAll := map[string]struct{}{}
	var totalMS int64

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec struct {
			Email            string  `json:"email"`
			Status           string  `json:"status"`
			SubStatus        *string `json:"sub_status"`
			DurationMS       int64   `json:"duration_ms"`
			IsCatchAllDomain bool    `json:"is_catch_all_domain"`
			Reason           *string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Status == "" {
			rec.Status = "unknown"
		}

		s.Total++
		s.Counts[rec.Status]++
		sub := ""
		if rec.SubStatus != nil && *rec.SubStatus != "" {
			sub = *rec.SubStatus
			s.SubCounts[sub]++
		}
		totalMS += rec.DurationMS

		if rec.IsCatchAllDomain {
			if i := strings.IndexByte(rec.Email, '@'); i >= 0 && i < len(rec.Email)-1 {
				catchAll[rec.Email[i+1:]] = struct{}{}
			}
		}
		if (rec.Status == "error" || rec.Status == "unknown") && len(s.SampleErrors) < 10 {
			label := sub
			if label == "" {
				label = rec.Status
			}
			reason := "?"
			if rec.Reason != nil {
				reason = *rec.Reason
			}
			s.SampleErrors = append(s.SampleErrors, "  "+rec.Email+": ["+label+"] "+reason)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	if s.Total > 0 {
		s.AvgDurationMS = math.Round(float64(totalMS)/float64(s.Total)*10) / 10
	}
	for d := range catchAll {
		s.CatchAllDomains = append(s.CatchAllDomains, d)
	}
	slices.Sort(s.CatchAllDomains)
	return s, nil
}
