package parse

import (
	"compress/gzip"
	"io"
)

type TARGZParser struct {
	tarParser *TARParser
}

func init() {
	p := &TARGZParser{
		tarParser: &TARParser{},
	}
	RegisterParser(".tar.gz", p)
	RegisterParser(".tgz", p)
}

func (p *TARGZParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	gzReader, err := gzip.NewReader(r)
	if err != nil {
		return FallbackStringsParse(r, filePath, resultsChan)
	}
	defer gzReader.Close()

	return p.tarParser.Parse(gzReader, filePath, resultsChan)
}
