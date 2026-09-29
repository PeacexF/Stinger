package parse

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"strings"
)

type XLSXParser struct{}

func init() {
	RegisterParser(".xlsx", &XLSXParser{})
}

func (p *XLSXParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
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

	// xl/sharedStrings.xml contains text data
	// xl/worksheets/sheetN.xml contains grid data
	for _, f := range zipReader.File {
		nameLower := strings.ToLower(f.Name)

		isSharedStrings := nameLower == "xl/sharedstrings.xml"
		isSheet := strings.HasPrefix(nameLower, "xl/worksheets/sheet") && strings.HasSuffix(nameLower, ".xml")

		if isSharedStrings || isSheet {
			xmlFile, err := f.Open()
			if err != nil {
				continue
			}

			_ = p.parseXMLStream(xmlFile, filePath, resultsChan)
			xmlFile.Close()
		}
	}

	return nil
}

func (p *XLSXParser) parseXMLStream(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	decoder := xml.NewDecoder(r)

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// raw text inside XML tags
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

func (p *XLSXParser) parseFallback(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	return FallbackStringsParse(r, filePath, resultsChan)
}
