package parse

import (
	"bufio"
	"io"
	"strings"
)

type INIParser struct{}

func init() {
	RegisterParser(".ini", &INIParser{})
}

func (p *INIParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024) // 64KB line buffer
	scanner.Buffer(buf, 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") || (strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			continue
		}

		// either '=' or ':' as key-value
		delimIdx := strings.IndexAny(trimmed, "=:")
		if delimIdx != -1 {
			val := trimmed[delimIdx+1:]

			val = strings.Trim(val, ` '"`)

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

		// Fallback line scan
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
