package parse

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

type SQLiteParser struct{}

func init() {
	p := &SQLiteParser{}
	RegisterParser(".sqlite", p)
	RegisterParser(".sqlite3", p)
}

// SQLite3 Header layout
type sqliteHeader struct {
	Magic    [16]byte // "SQLite format 3\0"
	PageSize uint16   // db page size
}

func (p *SQLiteParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	rawBytes, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if len(rawBytes) < 100 {
		return errors.New("file size too small for valid SQLite header structure")
	}

	var header sqliteHeader
	if err := binary.Read(bytes.NewReader(rawBytes[:18]), binary.LittleEndian, &header); err != nil {
		return err
	}

	// Verify SQLite
	expectedMagic := []byte("SQLite format 3\x00")
	if !bytes.Equal(header.Magic[:], expectedMagic) {
		return FallbackStringsParse(bytes.NewReader(rawBytes), filePath, resultsChan)
	}

	p.extractSQLiteTextStreams(rawBytes, filePath, resultsChan)
	return nil
}

func (p *SQLiteParser) extractSQLiteTextStreams(data []byte, filePath string, resultsChan chan<- JobResult) {
	i := 100
	n := len(data)

	var asciiBuf bytes.Buffer

	for i < n {
		b := data[i]

		// printable UTF-8 / ASCII text strings
		if b >= 32 && b <= 126 {
			asciiBuf.WriteByte(b)
		} else {
			if asciiBuf.Len() >= 5 {
				p.scanAndSubmit(asciiBuf.String(), filePath, resultsChan)
			}
			asciiBuf.Reset()
		}
		i++
	}

	// Flush
	if asciiBuf.Len() >= 5 {
		p.scanAndSubmit(asciiBuf.String(), filePath, resultsChan)
	}
}

func (p *SQLiteParser) scanAndSubmit(text string, filePath string, resultsChan chan<- JobResult) {
	if EmailRegex.MatchString(text) {
		matches := EmailRegex.FindAllString(text, -1)
		for _, email := range matches {
			resultsChan <- JobResult{
				FilePath: filePath,
				Email:    email,
			}
		}
	}
}
