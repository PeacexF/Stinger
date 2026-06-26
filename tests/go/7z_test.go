package tests

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/PeacexF/Stinger/smtp_stinger/parse"
)

func TestSevenZipParser_Registration(t *testing.T) {
	parser, exists := parse.ParserRegistry[".7z"]
	if !exists {
		t.Fatal("Expected SevenZipParser to be registered for '.7z'")
	}

	if _, ok := parser.(*parse.SevenZipParser); !ok {
		t.Errorf("Expected registered parser to be of type *SevenZipParser, got %T", parser)
	}
}

func TestSevenZipParser_Parse_FallbackOnNonFile(t *testing.T) {
	parser := &parse.SevenZipParser{}
	r := bytes.NewBufferString("not an os.File stream")
	resultsChan := make(chan parse.JobResult, 10)

	err := parser.Parse(r, "test.7z", resultsChan)
	if err != nil {
		t.Fatalf("Expected no error from FallbackStringsParse, got: %v", err)
	}
}

func TestSevenZipParser_Parse_Invalid7zFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "invalid_test_*.7z")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString("INVALID_SIGNATURE"); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Failed to seek temp file: %v", err)
	}

	parser := &parse.SevenZipParser{}
	resultsChan := make(chan parse.JobResult, 10)

	err = parser.Parse(tmpFile, tmpFile.Name(), resultsChan)
	if err != nil {
		t.Fatalf("Expected FallbackStringsParse fallback without error, got: %v", err)
	}
}

func TestSevenZipParser_Parse_ValidHeaderEmptyBody(t *testing.T) {
	// 7z signature header bytes
	sevenZipHeader := []byte{0x37, 0x7a, 0xbc, 0xaf, 0x27, 0x1c, 0x00, 0x04}

	tmpFile, err := os.CreateTemp("", "valid_header_*.7z")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(sevenZipHeader); err != nil {
		t.Fatalf("Failed to write header: %v", err)
	}
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Failed to seek temp file: %v", err)
	}

	parser := &parse.SevenZipParser{}
	resultsChan := make(chan parse.JobResult, 10)

	_ = parser.Parse(tmpFile, tmpFile.Name(), resultsChan)
}
