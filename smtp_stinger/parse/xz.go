package parse

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

type XZParser struct{}

func init() {
	RegisterParser(".xz", &XZParser{})
}

func (p *XZParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	xzReader, err := xz.NewReader(r)
	if err != nil {
		return FallbackStringsParse(r, filePath, resultsChan)
	}

	baseName := strings.TrimSuffix(filePath, filepath.Ext(filePath))
	innerExt := strings.ToLower(filepath.Ext(baseName))

	parser, exists := ParserRegistry[innerExt]
	if !exists {
		parser = &FallbackParser{}
	}

	limitedReader := io.LimitReader(xzReader, MaxUncompressedFileSize)

	var buffer bytes.Buffer
	_, err = io.Copy(&buffer, limitedReader)
	if err != nil {
		return err
	}

	seekableReader := bytes.NewReader(buffer.Bytes())
	return parser.Parse(seekableReader, baseName, resultsChan)
}
