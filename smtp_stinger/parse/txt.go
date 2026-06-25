package parse

import (
	"bufio"
	"io"
)

type TXTParser struct{}

func init() {
	parser := &TXTParser{}
	RegisterParser(".txt", parser)
	RegisterParser(".log", parser)
	RegisterParser(".tsv", parser)
}

func (p *TXTParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
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
