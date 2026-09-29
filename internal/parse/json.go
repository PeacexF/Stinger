package parse

import (
	"bufio"
	"encoding/json"
	"io"
)

type JSONParser struct{}

func init() {
	parser := &JSONParser{}
	RegisterParser(".json", parser)
	RegisterParser(".jsonl", parser)
}

func (p *JSONParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	br := bufio.NewReader(r)
	firstByte, err := br.Peek(1)
	if err != nil && err != io.EOF {
		return err
	}

	if len(firstByte) > 0 && (firstByte[0] == '{' || firstByte[0] == '[') {
		return p.parseStructuredJSON(br, filePath, resultsChan)
	}

	return p.parseJSONLines(br, filePath, resultsChan)
}

// parseStructuredJSON handles large structured .json objects/arrays via a token stream
func (p *JSONParser) parseStructuredJSON(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	decoder := json.NewDecoder(r)

	for {
		t, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// If structured parsing fails mid-way due to corruption, fallback to line extraction
			return p.parseJSONLines(r, filePath, resultsChan)
		}

		// We only care about actual string values inside the JSON structure
		if strValue, ok := t.(string); ok {
			if len(strValue) >= 5 && EmailRegex.MatchString(strValue) {
				matches := EmailRegex.FindAllString(strValue, -1)
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

// parseJSONLines handles .jsonl formats or malformed blocks line-by-line efficiently
func (p *JSONParser) parseJSONLines(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		matches := EmailRegex.FindAllString(line, -1)
		for _, email := range matches {
			resultsChan <- JobResult{
				FilePath: filePath,
				Email:    email,
			}
		}
	}
	return scanner.Err()
}
