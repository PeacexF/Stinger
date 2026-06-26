package tests

import (
	"bytes"
	"testing"

	"github.com/PeacexF/Stinger/smtp_stinger/parse"
)

func TestCSVParser_Registration(t *testing.T) {
	parser, exists := parse.ParserRegistry[".csv"]
	if !exists {
		t.Fatal("Expected CSVParser to be registered for '.csv'")
	}

	if _, ok := parser.(*parse.CSVParser); !ok {
		t.Errorf("Expected registered parser to be of type *CSVParser, got %T", parser)
	}
}

func TestCSVParser_Parse_ValidCSVWithEmails(t *testing.T) {
	parser := &parse.CSVParser{}

	csvData := "test@example.com,short,abc\n" +
		"valid@domain.org,another.valid+filter@gmail.com,123\n" +
		"no-email-here,processing,data\n"

	r := bytes.NewBufferString(csvData)
	resultsChan := make(chan parse.JobResult, 10)

	err := parser.Parse(r, "test.csv", resultsChan)
	if err != nil {
		t.Fatalf("Expected no error during CSV parsing, got: %v", err)
	}
	close(resultsChan)

	expectedEmails := map[string]bool{
		"test@example.com":               true,
		"valid@domain.org":               true,
		"another.valid+filter@gmail.com": true,
	}

	count := 0
	for res := range resultsChan {
		count++
		if res.FilePath != "test.csv" {
			t.Errorf("Expected FilePath 'test.csv', got '%s'", res.FilePath)
		}
		if !expectedEmails[res.Email] {
			t.Errorf("Unexpected email found: %s", res.Email)
		}
	}

	if count != 3 {
		t.Errorf("Expected 3 email matches, got %d", count)
	}
}

func TestCSVParser_Parse_MalformedCSVFallback(t *testing.T) {
	parser := &parse.CSVParser{}

	// Bare quotes forcing fallback
	malformedCSV := `header1,header2` + "\n" + `wrong"field,valid@fallback.com`

	r := bytes.NewBufferString(malformedCSV)
	resultsChan := make(chan parse.JobResult, 10)

	// csv.Reader fails, delegates to TXTParser
	// verify that the chain completes
	err := parser.Parse(r, "malformed.csv", resultsChan)
	if err != nil {
		t.Fatalf("Expected fallback to TXTParser to handle malformed CSV smoothly, got error: %v", err)
	}
}
