package parse

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// prevent Zip Bombs
const (
	MaxUncompressedFileSize = 50 * 1024 * 1024
)

type ZIPParser struct{}

func init() {
	RegisterParser(".zip", &ZIPParser{})
}

func (p *ZIPParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	file, ok := r.(*os.File)
	if !ok {
		return FallbackStringsParse(r, filePath, resultsChan)
	}

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	zipReader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return FallbackStringsParse(file, filePath, resultsChan)
	}

	for _, f := range zipReader.File {
		// Skip directory entries
		if f.FileInfo().IsDir() {
			continue
		}

		// Check uncompressed size headers
		if f.UncompressedSize64 > MaxUncompressedFileSize {
			continue
		}

		err := p.processZipEntry(f, filePath, resultsChan)
		if err != nil {
			continue
		}
	}

	return nil
}

func (p *ZIPParser) processZipEntry(f *zip.File, archivePath string, resultsChan chan<- JobResult) error {
	ext := strings.ToLower(filepath.Ext(f.Name))

	// internal file track
	virtualPath := archivePath + " -> " + f.Name

	parser, exists := ParserRegistry[ext]

	if !exists {
		parser = &FallbackParser{}
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	limitedReader := io.LimitReader(rc, MaxUncompressedFileSize)

	// copy the decompressed bytes into a buffer to give parsers like PDF or Office formats full capabilities
	var buffer bytes.Buffer
	_, err = io.Copy(&buffer, limitedReader)
	if err != nil {
		return err
	}

	seekableReader := bytes.NewReader(buffer.Bytes())

	return parser.Parse(seekableReader, virtualPath, resultsChan)
}

type FallbackParser struct{}

func (fp *FallbackParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	return FallbackStringsParse(r, filePath, resultsChan)
}
