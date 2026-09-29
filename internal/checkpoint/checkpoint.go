// Package checkpoint saves and loads the set of completed emails so an
// interrupted run can be resumed.
//
// line 1 — header:  {"type":"header","emails_file":"...","total":45797,"completed":120,"timestamp":"..."}
// next lines — one completed email per line:  {"type":"email","email":"alice@example.com"}
package checkpoint

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const Filename = "checkpoint.jsonl"

type Checkpoint struct {
	Path       string
	EmailsFile string
	Total      int

	mu        sync.Mutex
	completed map[string]struct{}
}

type record struct {
	Type       string `json:"type"`
	Email      string `json:"email,omitempty"`
	EmailsFile string `json:"emails_file,omitempty"`
	Total      *int   `json:"total,omitempty"`
	Completed  *int   `json:"completed,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
}

func New(outputDir, emailsFile string, total int) *Checkpoint {
	return &Checkpoint{
		Path:       filepath.Join(outputDir, Filename),
		EmailsFile: emailsFile,
		Total:      total,
		completed:  make(map[string]struct{}),
	}
}

// Mark records email as completed. Safe for concurrent use.
func (c *Checkpoint) Mark(email string) {
	c.mu.Lock()
	c.completed[email] = struct{}{}
	c.mu.Unlock()
}

// Completed returns a copy of the completed set
func (c *Checkpoint) Completed() map[string]struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.completed)
}

// Save atomically writes the checkpoint (via a .tmp file and rename)
func (c *Checkpoint) Save() error {
	c.mu.Lock()
	emails := make([]string, 0, len(c.completed))
	for e := range c.completed {
		emails = append(emails, e)
	}
	c.mu.Unlock()
	slices.Sort(emails)

	tmp := strings.TrimSuffix(c.Path, filepath.Ext(c.Path)) + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)

	total, done := c.Total, len(emails)
	err = enc.Encode(record{
		Type:       "header",
		EmailsFile: c.EmailsFile,
		Total:      &total,
		Completed:  &done,
		Timestamp:  time.Now().UTC().Format("2006-01-02T15:04:05.000000-07:00"),
	})
	for _, e := range emails {
		if err != nil {
			break
		}
		err = enc.Encode(record{Type: "email", Email: e})
	}
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, c.Path)
}

// Delete removes the checkpoint file if it exists
func (c *Checkpoint) Delete() error {
	err := os.Remove(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Load reads the completed emails from a checkpoint file
func Load(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("checkpoint file not found: %s", path)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	completed := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("checkpoint file is corrupt (line %d): %v", n, err)
		}
		if rec.Type == "email" {
			if e := strings.TrimSpace(rec.Email); e != "" {
				completed[e] = struct{}{}
			}
		}
	}
	return completed, sc.Err()
}
