package parse

import (
	"bytes"
	"io"
)

type DBParser struct {
	sqliteParser *SQLiteParser
}

func init() {
	RegisterParser(".db", &DBParser{
		sqliteParser: &SQLiteParser{},
	})
}

func (p *DBParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	rawBytes, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	sqliteMagic := []byte("SQLite format 3\x00")
	if len(rawBytes) >= 16 && bytes.Equal(rawBytes[:16], sqliteMagic) {
		return p.sqliteParser.Parse(bytes.NewReader(rawBytes), filePath, resultsChan)
	}

	return FallbackStringsParse(bytes.NewReader(rawBytes), filePath, resultsChan)
}
