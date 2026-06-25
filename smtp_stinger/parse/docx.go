package parse

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
)

type DOCXParser struct{}

func init() {
	RegisterParser(".docx", &DOCXParser{})
}

func (p *DOCXParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	// Because zip.NewReader needs an io.ReaderAt and the size of the file, we cannot purely stream from a raw io.Reader interface if it doesn't support seeking
	// Since our main loop opens files via os.Open, we check if we can handle it as a concrete file descriptor
	file, ok := r.(*os.File)
	if !ok {
		return p.parseFallback(r, filePath, resultsChan)
	}

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	zipReader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return p.parseFallback(file, filePath, resultsChan)
	}

	for _, f := range zipReader.File {
		if f.Name == "word/document.xml" {
			xmlFile, err := f.Open()
			if err != nil {
				return err
			}

			err = p.parseXMLStream(xmlFile, filePath, resultsChan)
			xmlFile.Close()
			return err
		}
	}

	// If document.xml wasn't found, it might be a flat XML, run a full raw text extraction
	_, _ = file.Seek(0, io.SeekStart)
	return p.parseFallback(file, filePath, resultsChan)
}

func (p *DOCXParser) parseXMLStream(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	decoder := xml.NewDecoder(r)

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if cd, ok := token.(xml.CharData); ok {
			textStr := string(cd)
			if len(textStr) >= 5 && EmailRegex.MatchString(textStr) {
				matches := EmailRegex.FindAllString(textStr, -1)
				for _, email := range matches {
					resultsChan <- JobResult{
						FilePath: filePath,
						Email:    email,
					}
				}
			}
		}
	}
	return nil
}

func (p *DOCXParser) parseFallback(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	// Re-route processing
	return FallbackStringsParse(r, filePath, resultsChan)
}
