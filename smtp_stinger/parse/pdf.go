package parse

import (
	"bytes"
	"io"
	"os"

	"github.com/dslipak/pdf"
)

type PDFParser struct{}

func init() {
	RegisterParser(".pdf", &PDFParser{})
}

func (p *PDFParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	file, ok := r.(*os.File)
	if !ok {
		return p.parseFallback(r, filePath, resultsChan)
	}

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	// read and open the PDF layout
	pdfReader, err := pdf.NewReader(file, stat.Size())
	if err != nil {
		return p.parseFallback(file, filePath, resultsChan)
	}

	numPages := pdfReader.NumPage()
	var textBuf bytes.Buffer

	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page := pdfReader.Page(pageNum)
		textStr, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}

		textBuf.WriteString(textStr)

		if textBuf.Len() >= 4096 || pageNum == numPages {
			fullText := textBuf.String()
			if EmailRegex.MatchString(fullText) {
				matches := EmailRegex.FindAllString(fullText, -1)
				for _, email := range matches {
					resultsChan <- JobResult{
						FilePath: filePath,
						Email:    email,
					}
				}
			}
			textBuf.Reset() // Flush
		}
	}

	return nil
}

func (p *PDFParser) parseFallback(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	return FallbackStringsParse(r, filePath, resultsChan)
}
