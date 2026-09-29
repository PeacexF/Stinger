package parse

import (
	"bytes"
	"compress/gzip"
	"io"
	"path/filepath"
	"strings"
)

type GZParser struct{}

func init() {
	RegisterParser(".gz", &GZParser{})
}

func (p *GZParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	gzReader, err := gzip.NewReader(r)
	if err != nil {
		return FallbackStringsParse(r, filePath, resultsChan)
	}
	defer gzReader.Close()

	baseName := strings.TrimSuffix(filePath, filepath.Ext(filePath))
	innerExt := strings.ToLower(filepath.Ext(baseName))

	parser, exists := ParserRegistry[innerExt]
	if !exists {
		parser = &FallbackParser{}
	}

	limitedReader := io.LimitReader(gzReader, MaxUncompressedFileSize)

	var buffer bytes.Buffer
	_, err = io.Copy(&buffer, limitedReader)
	if err != nil {
		return err
	}

	seekableReader := bytes.NewReader(buffer.Bytes())
	return parser.Parse(seekableReader, baseName, resultsChan)
}
