package parse

import (
	"bufio"
	"io"
	"strings"
)

type TOMLParser struct{}

func init() {
	RegisterParser(".toml", &TOMLParser{})
}

func (p *TOMLParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024) // 64KB line buffer
	scanner.Buffer(buf, 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || (strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			continue
		}

		// Look for key-value delimiter (email = "target@domain.com")
		if idx := strings.Index(trimmed, "="); idx != -1 {
			val := trimmed[idx+1:]

			val = strings.Trim(val, ` '"{}[]`)

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

		// Scan the full raw line anyway to capture emails
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
