package parse

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"

	"github.com/nwaples/rardecode/v2"
)

type RARParser struct{}

func init() {
	RegisterParser(".rar", &RARParser{})
}

func (p *RARParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	rarReader, err := rardecode.NewReader(r)
	if err != nil {
		return FallbackStringsParse(r, filePath, resultsChan)
	}

	for {
		hdr, err := rarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if hdr.IsDir {
			continue
		}

		err = p.processRarEntry(rarReader, hdr.Name, filePath, resultsChan)
		if err != nil {
			continue
		}
	}

	return nil
}

func (p *RARParser) processRarEntry(rarReader *rardecode.Reader, innerName string, archivePath string, resultsChan chan<- JobResult) error {
	ext := strings.ToLower(filepath.Ext(innerName))
	virtualPath := archivePath + " -> " + innerName

	parser, exists := ParserRegistry[ext]
	if !exists {
		parser = &FallbackParser{}
	}

	limitedReader := io.LimitReader(rarReader, MaxUncompressedFileSize)

	var buffer bytes.Buffer
	_, err := io.Copy(&buffer, limitedReader)
	if err != nil {
		return err
	}

	seekableReader := bytes.NewReader(buffer.Bytes())

	return parser.Parse(seekableReader, virtualPath, resultsChan)
}
