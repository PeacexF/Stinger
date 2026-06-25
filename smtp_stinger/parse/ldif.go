package parse

import (
	"bufio"
	"encoding/base64"
	"io"
	"strings"
)

type LDIFParser struct{}

func init() {
	RegisterParser(".ldif", &LDIFParser{})
}

func (p *LDIFParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	scanner := bufio.NewScanner(r)

	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// standard | base64 encoded attributes
		if idx := strings.Index(trimmed, ":"); idx != -1 {
			isBase64 := false
			valIdx := idx + 1

			if valIdx < len(trimmed) && trimmed[valIdx] == ':' {
				isBase64 = true
				valIdx++
			}

			value := strings.TrimSpace(trimmed[valIdx:])
			if value == "" {
				continue
			}

			if isBase64 {
				// Decode base64 data string
				decodedBytes, err := base64.StdEncoding.DecodeString(value)
				if err == nil {
					p.extractEmails(string(decodedBytes), filePath, resultsChan)
				}
			}
		}

		p.extractEmails(line, filePath, resultsChan)
	}

	return scanner.Err()
}

func (p *LDIFParser) extractEmails(text string, filePath string, resultsChan chan<- JobResult) {
	if EmailRegex.MatchString(text) {
		matches := EmailRegex.FindAllString(text, -1)
		for _, email := range matches {
			resultsChan <- JobResult{
				FilePath: filePath,
				Email:    email,
			}
		}
	}
}
