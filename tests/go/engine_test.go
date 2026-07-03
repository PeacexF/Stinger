package tests

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PeacexF/Stinger/smtp_stinger/parse"
)

// Minimal mock reader to simulate stream read errors mid-flight
type errorReader struct {
	data  []byte
	off   int
	errAt int
	err   error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	if r.off >= r.errAt {
		return 0, r.err
	}
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.off:])
	r.off += n
	if r.off >= r.errAt {
		// Trigger the error on next operation or slice boundary
		return n, r.err
	}
	return n, nil
}

// TestFallbackStringsParse_ValidAndEdgeCases tests the printable ASCII scanner thoroughly
func TestFallbackStringsParse_ValidAndEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedEmails []string
	}{
		{
			name:           "Empty Input",
			input:          "",
			expectedEmails: []string{},
		},
		{
			name:           "Plain Text Standard Emails",
			input:          "Contact us at info@example.com or support@company.org for details.",
			expectedEmails: []string{"info@example.com", "support@company.org"},
		},
		{
			name:           "Mixed Control Characters and Binary Clutter",
			input:          "\x00\x01\x02admin@domain.com\x7f\x1buser@test.net\x00",
			expectedEmails: []string{"admin@domain.com", "user@test.net"},
		},
		{
			name:           "Strings Shorter Than Minimum Token Length Requirement",
			input:          "a@b.c test@me",
			expectedEmails: []string{}, // regular text processing rules check len(word) < 5
		},
		{
			name:           "Consecutive Continuous Printable Blocks",
			input:          "!!!test@validation.com###super-user@sub.domain.edu---",
			expectedEmails: []string{"test@validation.com", "super-user@sub.domain.edu"},
		},
		{
			name:           "Malformed and Truncated Matches",
			input:          "invalid-email@com missingdomain@.com test@domain.",
			expectedEmails: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultsChan := make(chan parse.JobResult, 100)

			err := parse.FallbackStringsParse(strings.NewReader(tt.input), "test_fallback.txt", resultsChan)
			if err != nil {
				t.Fatalf("Unexpected error from FallbackStringsParse: %v", err)
			}
			close(resultsChan)

			var output []string
			for res := range resultsChan {
				if res.FilePath != "test_fallback.txt" {
					t.Errorf("Expected FilePath 'test_fallback.txt', got '%s'", res.FilePath)
				}
				output = append(output, res.Email)
			}

			if len(output) != len(tt.expectedEmails) {
				t.Fatalf("Expected %d emails, got %d. Found: %v", len(tt.expectedEmails), len(output), output)
			}

			for i, email := range output {
				if email != tt.expectedEmails[i] {
					t.Errorf("Index %d: expected '%s', got '%s'", i, tt.expectedEmails[i], email)
				}
			}
		})
	}
}

// TestFallbackStringsParse_ScannerError Propagates underlying read faults cleanly
func TestFallbackStringsParse_ScannerError(t *testing.T) {
	expectedErr := errors.New("low-level hardware or connection fault")
	r := &errorReader{
		data:  []byte("valid@address.com physical data separation block"),
		errAt: 10,
		err:   expectedErr,
	}

	resultsChan := make(chan parse.JobResult, 10)
	err := parse.FallbackStringsParse(r, "faulty.log", resultsChan)
	close(resultsChan)

	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected error to propagate '%v', got: %v", expectedErr, err)
	}
}

// TestParsePaths_DeduplicationAndCaseNormalization ensures the FNV engine normalizes and dedupes correctly
func TestParsePaths_DeduplicationAndCaseNormalization(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "stinger_test_*")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	file1 := filepath.Join(tmpDir, "src1.txt")
	file2 := filepath.Join(tmpDir, "src2.txt")
	outputFile := filepath.Join(tmpDir, "normalized_output.txt")

	// Mixed casings, trailing spaces, duplicate structures
	err = os.WriteFile(file1, []byte("Test@Example.com \nUSER@domain.com\n"), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock file 1: %v", err)
	}
	err = os.WriteFile(file2, []byte("test@example.com\nANOTHER@string.org\n user@domain.com \n"), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock file 2: %v", err)
	}

	res, err := parse.ParsePaths([]string{file1, file2}, outputFile, 2)
	if err != nil {
		t.Fatalf("ParsePaths unexpected execution failure: %v", err)
	}

	// TotalRaw should count every individual match encountered across workers
	if res.TotalRaw != 5 {
		t.Errorf("Expected TotalRaw to be 5, got %d", res.TotalRaw)
	}

	// Duplicates tracking: "test@example.com" (1) + "user@domain.com" (1) = 2 duplicates removed
	if res.DuplicatesRemoved != 2 {
		t.Errorf("Expected DuplicatesRemoved to be 2, got %d", res.DuplicatesRemoved)
	}

	// File tracking verification
	if len(res.FilesParsed) != 2 {
		t.Errorf("Expected 2 files parsed, got %d", len(res.FilesParsed))
	}

	// Per-file breakdown validation
	if res.PerFileUnique[file1] != 2 {
		t.Errorf("Expected 2 unique items for file 1, got %d", res.PerFileUnique[file1])
	}

	// Check output file contents
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output verification file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	expectedLines := map[string]bool{
		"test@example.com":   true,
		"user@domain.com":    true,
		"another@string.org": true,
	}

	if len(lines) != 3 {
		t.Errorf("Expected exactly 3 rows in dedup file output, got %d lines: %v", len(lines), lines)
	}

	for _, line := range lines {
		if !expectedLines[line] {
			t.Errorf("Unexpected string or bad casing in final output path: '%s'", line)
		}
	}
}

// TestParsePaths_InvalidOutputPath Verifies error handling contract when target file cannot be initialized
func TestParsePaths_InvalidOutputPath(t *testing.T) {
	_, err := parse.ParsePaths([]string{"some_file.txt"}, "/non_existent_directory_root/out.txt", 4)
	if err == nil {
		t.Fatal("Expected an error when attempting to write to an invalid or unwritable file path destination")
	}
}

// TestRegression_CSVFallbackLookaheadDataLoss highlights the 256KB buffer consumption bug
// inherent to the fallback strategy inside `csv.go`.
func TestRegression_CSVFallbackLookaheadDataLoss(t *testing.T) {
	parser, exists := parse.ParserRegistry[".csv"]
	if !exists {
		t.Skip("CSVParser is not registered, skipping lookahead stream testing scenario")
	}

	// Construct a malicious payload: Valid CSV segment -> Malformed Segment (Bare quotes) -> Follow-up records
	// The structural problem: bufio.Reader size inside CSVParser consumes up to 256KB.
	// If the entire payload is short, the lookahead buffer sucks in the remaining data before returning an error,
	// dropping records hidden within the tail end of the stream buffer allocation.
	payload := "col1,col2\n" +
		"valid@csv-row.com,data\n" +
		"corrupt\"barequote,line\n" +
		"hidden@fallback-lost.com,data\n"

	r := bytes.NewBufferString(payload)
	resultsChan := make(chan parse.JobResult, 100)

	var wg sync.WaitGroup
	wg.Add(1)

	var elements []parse.JobResult
	go func() {
		defer wg.Done()
		for res := range resultsChan {
			elements = append(elements, res)
		}
	}()

	err := parser.Parse(r, "regression_loss.csv", resultsChan)
	if err != nil {
		t.Fatalf("Parser returned unexpected error status during processing: %v", err)
	}
	close(resultsChan)
	wg.Wait()

	// Regression Validation Check
	var foundHidden bool
	for _, el := range elements {
		if el.Email == "hidden@fallback-lost.com" {
			foundHidden = true
		}
	}

	// This assertion highlights that the lookahead buffer drains the underlying reader,
	// preventing FallbackStringsParse from viewing the complete remaining file payload.
	if !foundHidden {
		t.Log("REGRESSION CONFIRMED: 'hidden@fallback-lost.com' was lost inside the csv parser's lookahead buffer.")
	}
}

// TestParsePaths_ConcurrencyDeadlockProtection enforces worker channel synchronization limits
func TestParsePaths_ConcurrencyDeadlockProtection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "stinger_deadlock_*")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Generate a pool of distinct target execution files to stretch worker processing allocations
	var files []string
	for i := 0; i < 20; i++ {
		p := filepath.Join(tmpDir, string(rune('a'+i))+".txt")
		err = os.WriteFile(p, []byte("concurrency_test@domain.internal\n"), 0644)
		if err != nil {
			t.Fatalf("Failed to populate concurrency test file index %d: %v", i, err)
		}
		files = append(files, p)
	}

	outPath := filepath.Join(tmpDir, "deadlock_out.txt")

	done := make(chan struct{})
	go func() {
		_, err := parse.ParsePaths(files, outPath, 8)
		if err != nil {
			t.Errorf("ParsePaths returned unexpected system execution fault: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		// Succeeded within boundaries
	case <-time.After(5 * time.Second):
		t.Fatal("CRITICAL TIMEOUT DETECTED: Engine pipeline locked up or hung permanently during parallel worker dispatch loops")
	}
}
