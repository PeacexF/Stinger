package parse

import (
	"bufio" // Добавили импорт bufio
	"encoding/csv"
	"io"
)

type CSVParser struct{}

func init() {
	RegisterParser(".csv", &CSVParser{})
}

func (p *CSVParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	bufferedSize := 256 * 1024
	br := bufio.NewReaderSize(r, bufferedSize)

	reader := csv.NewReader(br)
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// If CSV is malformed, pass the reader handle down to the text parser fallback
			txtParser := &TXTParser{}
			return txtParser.Parse(br, filePath, resultsChan)
		}

		for _, cell := range record {
			if len(cell) < 5 {
				continue
			}
			if EmailRegex.MatchString(cell) {
				matches := EmailRegex.FindAllString(cell, -1)
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
