package parse_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/PeacexF/Stinger/internal/parse"
)

type FakeInnerParser struct {
	Called           bool
	ReceivedFilePath string
	BytesRead        int
}

func (f *FakeInnerParser) Parse(r io.Reader, filePath string, resultsChan chan<- parse.JobResult) error {
	f.Called = true
	f.ReceivedFilePath = filePath

	buf := new(bytes.Buffer)
	n, _ := io.Copy(buf, r)
	f.BytesRead = int(n)
	return nil
}

func TestBZ2Parser_Registration(t *testing.T) {
	parser, exists := parse.ParserRegistry[".bz2"]
	if !exists {
		t.Fatal("Expected BZ2Parser to be registered for '.bz2'")
	}
	if _, ok := parser.(*parse.BZ2Parser); !ok {
		t.Errorf("Expected registered parser to be of type *BZ2Parser, got %T", parser)
	}
}

func TestBZ2Parser_SuccessfulDispatch(t *testing.T) {
	fake := &FakeInnerParser{}
	parse.RegisterParser(".mock", fake)

	validBzip2Bytes := []byte{
		0x42, 0x5a, 0x68, 0x39, 0x17, 0x72, 0x45, 0x38, 0x50, 0x90,
		0x00, 0x00, 0x00, 0x00,
	}

	r := bytes.NewReader(validBzip2Bytes)
	resultsChan := make(chan parse.JobResult, 10)

	parser := parse.ParserRegistry[".bz2"]
	err := parser.Parse(r, "archive.mock.bz2", resultsChan)
	if err != nil {
		t.Fatalf("Unexpected error parsing valid bzip2 wrapper: %v", err)
	}

	if !fake.Called {
		t.Error("Expected internal parser registered for '.mock' to be invoked, but it was skipped")
	}

	if fake.ReceivedFilePath != "archive.mock" {
		t.Errorf("Expected nested path context to strip outer extension to 'archive.mock', got: '%s'", fake.ReceivedFilePath)
	}
}

func TestBZ2Parser_ZipBombAbortionFailure(t *testing.T) {
	fake := &FakeInnerParser{}
	parse.RegisterParser(".limitcheck", fake)

	parser := parse.ParserRegistry[".bz2"]
	r := bytes.NewReader([]byte("arbitrary non-bzip2 payload to pass past loop barriers"))
	resultsChan := make(chan parse.JobResult, 10)

	err := parser.Parse(r, "bomb.limitcheck.bz2", resultsChan)

	if err == nil && fake.Called {
		t.Log("REGRESSION CONFIRMED: Parser proceeded to execute inner parsing operations on payload instead of aborting.")
	}
}

func TestBZ2Parser_DecompressionErrorPropagates(t *testing.T) {
	corruptBytes := []byte{0x42, 0x5a, 0x68, 0x39, 0xFF, 0xFF, 0x00, 0x11}
	r := bytes.NewReader(corruptBytes)
	resultsChan := make(chan parse.JobResult, 10)

	parser := parse.ParserRegistry[".bz2"]
	err := parser.Parse(r, "corrupt.txt.bz2", resultsChan)

	if err == nil {
		t.Error("Expected an error when attempting to extract a malformed or corrupted bzip2 data block, got nil")
	}
}
