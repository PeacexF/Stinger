package parse

import (
	"bufio"
	"io"
	"strings"
)

type MBOXParser struct{}

func init() {
	RegisterParser(".mbox", &MBOXParser{})
}

func (p *MBOXParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	scanner := bufio.NewScanner(r)

	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 64*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "From ") {
			p.extractEmailsFromLine(line, filePath, resultsChan)
			continue
		}

		if len(line) >= 5 && EmailRegex.MatchString(line) {
			p.extractEmailsFromLine(line, filePath, resultsChan)
		}
	}

	return scanner.Err()
}

func (p *MBOXParser) extractEmailsFromLine(line string, filePath string, resultsChan chan<- JobResult) {
	matches := EmailRegex.FindAllString(line, -1)
	for _, email := range matches {
		resultsChan <- JobResult{
			FilePath: filePath,
			Email:    email,
		}
	}
}
