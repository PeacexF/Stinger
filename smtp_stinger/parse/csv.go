package stinger

import (
	"encoding/csv"
	"io"
	"os"
)

func ParseCSV(filePath string, resultsChan chan<- JobResult) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Malformed CSV fallback
			return ParseTXT(filePath, resultsChan)
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
