package parse

import (
	"bufio"
	"io"
	"strings"
)

type VCFParser struct{}

func init() {
	RegisterParser(".vcf", &VCFParser{})
}

func (p *VCFParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	scanner := bufio.NewScanner(r)

	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "BEGIN:") || strings.HasPrefix(trimmed, "END:") {
			continue
		}

		// Target structural email markers
		if strings.Contains(trimmed, "EMAIL") {
			if idx := strings.Index(trimmed, ":"); idx != -1 {
				emailVal := strings.TrimSpace(trimmed[idx+1:])
				p.extractEmails(emailVal, filePath, resultsChan)
				continue
			}
		}

		p.extractEmails(line, filePath, resultsChan)
	}

	return scanner.Err()
}

func (p *VCFParser) extractEmails(text string, filePath string, resultsChan chan<- JobResult) {
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
