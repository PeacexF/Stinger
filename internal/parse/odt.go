package parse

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"strings"
)

type ODTParser struct{}

func init() {
	RegisterParser(".odt", &ODTParser{})
}

func (p *ODTParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
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
		if strings.ToLower(f.Name) == "content.xml" { // main content body
			xmlFile, err := f.Open()
			if err != nil {
				return err
			}

			err = p.parseXMLStream(xmlFile, filePath, resultsChan)
			xmlFile.Close()
			return err
		}
	}

	_, _ = file.Seek(0, io.SeekStart)
	return p.parseFallback(file, filePath, resultsChan)
}

func (p *ODTParser) parseXMLStream(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
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

func (p *ODTParser) parseFallback(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	return FallbackStringsParse(r, filePath, resultsChan)
}
