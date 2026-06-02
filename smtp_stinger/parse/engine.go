package stinger

import (
	"bufio"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// RFC-5321
var EmailRegex = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

type ParseResult struct {
	TotalRaw          int64
	DuplicatesRemoved int64
	FilesParsed       []string
	FilesSkipped      []string
	PerFileUnique     map[string]int64
}

type JobResult struct {
	FilePath string
	Email    string
}

// ParsePaths coordinates concurrent parsing and sequential deduplication.
func ParsePaths(paths []string, outputPath string, maxWorkers int) (*ParseResult, error) {
	outFunc, err := os.Create(outputPath)
	if err != nil {
		return nil, err
	}
	defer outFunc.Close()
	writer := bufio.NewWriterSize(outFunc, 128*1024) // 128KB buffer
	defer writer.Flush()

	resultsChan := make(chan JobResult, 10000)
	var workerWg sync.WaitGroup
	jobChan := make(chan string, len(paths))

	// Start a pool of parsing workers
	for i := 0; i < maxWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for path := range jobChan {
				ext := strings.ToLower(filepath.Ext(path))
				if ext == ".csv" {
					_ = ParseCSV(path, resultsChan)
				} else {
					_ = ParseTXT(path, resultsChan)
				}
			}
		}()
	}

	// Queue the files
	for _, path := range paths {
		jobChan <- path
	}
	close(jobChan)

	// Single-threaded Consumer/Deduplicator to avoid race conditions and file locks
	result := &ParseResult{
		PerFileUnique: make(map[string]int64),
		FilesParsed:   []string{},
	}

	// Track uniqueness using a 64-bit hash map
	seenHashes := make(map[uint64]struct{})
	parsedFilesMap := make(map[string]bool)

	var dedupWg sync.WaitGroup
	dedupWg.Add(1)
	go func() {
		defer dedupWg.Done()
		hasher := fnv.New64a()

		for res := range resultsChan {
			result.TotalRaw++
			if !parsedFilesMap[res.FilePath] {
				parsedFilesMap[res.FilePath] = true
				result.FilesParsed = append(result.FilesParsed, res.FilePath)
			}

			// Clean and hash the email
			normalized := strings.ToLower(strings.TrimSpace(res.Email))
			hasher.Reset()
			_, _ = hasher.Write([]byte(normalized))
			hash := hasher.Sum64()

			if _, exists := seenHashes[hash]; !exists {
				seenHashes[hash] = struct{}{}
				result.PerFileUnique[res.FilePath]++
				_, _ = writer.WriteString(normalized + "\n")
			} else {
				result.DuplicatesRemoved++
			}
		}
	}()

	// Wait for workers to finish parsing, then close the results channel
	workerWg.Wait()
	close(resultsChan)

	// Wait for the deduplicator to finish writing
	dedupWg.Wait()

	return result, nil
}
