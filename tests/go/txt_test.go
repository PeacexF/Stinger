package tests

import (
	"bytes"
	"errors"
	"testing"

	"github.com/PeacexF/Stinger/smtp_stinger/parse"
)

// simulates an io.Reader that returns an error
type ErroneousReader struct{}

func (e *ErroneousReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated read error")
}

func TestTXTParser_Registration(t *testing.T) {
	extensions := []string{".txt", ".log", ".tsv"}

	for _, ext := range extensions {
		parser, exists := parse.ParserRegistry[ext]
		if !exists {
			t.Fatalf("Expected TXTParser to be registered for '%s'", ext)
		}

		if _, ok := parser.(*parse.TXTParser); !ok {
			t.Errorf("Expected registered parser for '%s' to be of type *TXTParser, got %T", ext, parser)
		}
	}
}

func TestTXTParser_Parse_ValidTextWithEmails(t *testing.T) {
	parser := &parse.TXTParser{}

	textContent := "admin@site.com\n" +
		"some random text here\n" +
		"support@company.org mixed within text, info@domain.io\n"

	r := bytes.NewBufferString(textContent)
	resultsChan := make(chan parse.JobResult, 10)

	err := parser.Parse(r, "test.log", resultsChan)
	if err != nil {
		t.Fatalf("Expected no error during text parsing, got: %v", err)
	}
	close(resultsChan)

	expectedEmails := map[string]bool{
		"admin@site.com":      true,
		"support@company.org": true,
		"info@domain.io":      true,
	}

	count := 0
	for res := range resultsChan {
		count++
		if res.FilePath != "test.log" {
			t.Errorf("Expected FilePath 'test.log', got '%s'", res.FilePath)
		}
		if !expectedEmails[res.Email] {
			t.Errorf("Unexpected email found: %s", res.Email)
		}
	}

	if count != 3 {
		t.Errorf("Expected 3 email matches, got %d", count)
	}
}

func TestTXTParser_Parse_ScannerError(t *testing.T) {
	parser := &parse.TXTParser{}
	r := &ErroneousReader{}
	resultsChan := make(chan parse.JobResult, 10)

	err := parser.Parse(r, "error.txt", resultsChan)
	if err == nil {
		t.Fatal("Expected scanner error from ErroneousReader, got nil")
	}

	if err.Error() != "simulated read error" {
		t.Errorf("Expected 'simulated read error', got: %v", err)
	}
}
