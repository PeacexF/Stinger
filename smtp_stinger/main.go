// Parsing
package main

import (
	"encoding/json"
	"os"

	stinger "stinger/smtp_stinger/parse"
)

type Job struct {
	Paths      []string `json:"paths"`
	OutputPath string   `json:"output_path"`
	Workers    int      `json:"workers"`
}

type Result struct {
	TotalRaw          int64            `json:"total_raw"`
	DuplicatesRemoved int64            `json:"duplicates_removed"`
	Unique            int64            `json:"unique"`
	FilesParsed       []string         `json:"files_parsed"`
	FilesSkipped      []string         `json:"files_skipped"`
	PerFileUnique     map[string]int64 `json:"per_file_unique"`
	Error             string           `json:"error"`
}

func main() {
	var job Job
	if err := json.NewDecoder(os.Stdin).Decode(&job); err != nil {
		writeError("failed to decode job: " + err.Error())
		os.Exit(1)
	}

	if job.Workers <= 0 {
		job.Workers = 4
	}
	if job.OutputPath == "" {
		writeError("output_path is required")
		os.Exit(1)
	}
	if len(job.Paths) == 0 {
		writeError("paths must not be empty")
		os.Exit(1)
	}

	pr, err := stinger.ParsePaths(job.Paths, job.OutputPath, job.Workers)
	if err != nil {
		writeError(err.Error())
		os.Exit(1)
	}

	// Files that were passed but never appeared in FilesParsed are skipped
	parsedSet := make(map[string]bool, len(pr.FilesParsed))
	for _, f := range pr.FilesParsed {
		parsedSet[f] = true
	}
	var skipped []string
	for _, p := range job.Paths {
		if !parsedSet[p] {
			skipped = append(skipped, p)
		}
	}
	if skipped == nil {
		skipped = []string{}
	}
	if pr.FilesParsed == nil {
		pr.FilesParsed = []string{}
	}

	unique := pr.TotalRaw - pr.DuplicatesRemoved
	out := Result{
		TotalRaw:          pr.TotalRaw,
		DuplicatesRemoved: pr.DuplicatesRemoved,
		Unique:            unique,
		FilesParsed:       pr.FilesParsed,
		FilesSkipped:      skipped,
		PerFileUnique:     pr.PerFileUnique,
		Error:             "",
	}
	json.NewEncoder(os.Stdout).Encode(out)
}

func writeError(msg string) {
	json.NewEncoder(os.Stdout).Encode(Result{
		Error:         msg,
		FilesParsed:   []string{},
		FilesSkipped:  []string{},
		PerFileUnique: map[string]int64{},
	})
}
