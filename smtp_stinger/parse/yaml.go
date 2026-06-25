package parse

import (
	"bufio"
	"io"
	"strings"
)

type YAMLParser struct{}

func init() {
	parser := &YAMLParser{}
	RegisterParser(".yaml", parser)
	RegisterParser(".yml", parser)
}

func (p *YAMLParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024) // 64KB line buffer
	scanner.Buffer(buf, 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Look for standard YAML key-value  ("email: user@domain.com")
		if idx := strings.Index(trimmed, ":"); idx != -1 {
			// Extract the value side of the pair
			val := trimmed[idx+1:]

			// Clean off typical YAML syntax
			val = strings.Trim(val, ` '"{}[]-,`)

			if len(val) >= 5 && EmailRegex.MatchString(val) {
				matches := EmailRegex.FindAllString(val, -1)
				for _, email := range matches {
					resultsChan <- JobResult{
						FilePath: filePath,
						Email:    email,
					}
				}
				continue
			}
		}

		// If the line doesn't follow a strict 'key: value' layout
		if len(trimmed) >= 5 && EmailRegex.MatchString(trimmed) {
			matches := EmailRegex.FindAllString(trimmed, -1)
			for _, email := range matches {
				resultsChan <- JobResult{
					FilePath: filePath,
					Email:    email,
				}
			}
		}
	}
	return scanner.Err()
}
