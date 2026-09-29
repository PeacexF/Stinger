package parse

import (
	"bytes"
	"compress/bzip2" // the stdlib is so good in Go
	"io"
	"path/filepath"
	"strings"
)

type BZ2Parser struct{}

func init() {
	RegisterParser(".bz2", &BZ2Parser{})
}

func (p *BZ2Parser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	bz2Reader := bzip2.NewReader(r)

	baseName := strings.TrimSuffix(filePath, filepath.Ext(filePath))
	innerExt := strings.ToLower(filepath.Ext(baseName))

	parser, exists := ParserRegistry[innerExt]
	if !exists {
		parser = &FallbackParser{}
	}

	limitedReader := io.LimitReader(bz2Reader, MaxUncompressedFileSize)

	var buffer bytes.Buffer
	_, err := io.Copy(&buffer, limitedReader)
	if err != nil {
		return err
	}

	seekableReader := bytes.NewReader(buffer.Bytes())
	return parser.Parse(seekableReader, baseName, resultsChan)
}
