package parse

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/saracen/go7z"
)

type SevenZipParser struct{}

func init() {
	RegisterParser(".7z", &SevenZipParser{})
}

func (p *SevenZipParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	file, ok := r.(*os.File)
	if !ok {
		return FallbackStringsParse(r, filePath, resultsChan)
	}

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	szReader, err := go7z.NewReader(file, stat.Size())
	if err != nil {
		return FallbackStringsParse(file, filePath, resultsChan)
	}

	for {
		hdr, err := szReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if hdr.IsEmptyStream || hdr.IsEmptyFile {
			continue
		}

		// io.LimitReader already blocks zip bombs
		err = p.processSevenZipEntry(szReader, hdr.Name, filePath, resultsChan)
		if err != nil {
			continue
		}
	}

	return nil
}

func (p *SevenZipParser) processSevenZipEntry(szReader *go7z.Reader, innerName string, archivePath string, resultsChan chan<- JobResult) error {
	ext := strings.ToLower(filepath.Ext(innerName))
	virtualPath := archivePath + " -> " + innerName

	parser, exists := ParserRegistry[ext]
	if !exists {
		parser = &FallbackParser{}
	}

	limitedReader := io.LimitReader(szReader, MaxUncompressedFileSize)

	var buffer bytes.Buffer
	_, err := io.Copy(&buffer, limitedReader)
	if err != nil {
		return err
	}

	seekableReader := bytes.NewReader(buffer.Bytes())

	return parser.Parse(seekableReader, virtualPath, resultsChan)
}
