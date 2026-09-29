package parse

import (
	"archive/tar"
	"bytes"
	"io"
	"path/filepath"
	"strings"
)

type TARParser struct{}

func init() {
	RegisterParser(".tar", &TARParser{})
}

func (p *TARParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	tarReader := tar.NewReader(r)

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break // end of archive
		}
		if err != nil {
			return FallbackStringsParse(r, filePath, resultsChan)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		err = p.processTarEntry(tarReader, hdr.Name, filePath, resultsChan)
		if err != nil {
			continue
		}
	}

	return nil
}

func (p *TARParser) processTarEntry(tarReader *tar.Reader, innerName string, archivePath string, resultsChan chan<- JobResult) error {
	ext := strings.ToLower(filepath.Ext(innerName))
	virtualPath := archivePath + " -> " + innerName

	parser, exists := ParserRegistry[ext]
	if !exists {
		parser = &FallbackParser{}
	}

	limitedReader := io.LimitReader(tarReader, MaxUncompressedFileSize)

	var buffer bytes.Buffer
	_, err := io.Copy(&buffer, limitedReader)
	if err != nil {
		return err
	}

	seekableReader := bytes.NewReader(buffer.Bytes())

	return parser.Parse(seekableReader, virtualPath, resultsChan)
}
