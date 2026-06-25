package parse

import (
	"io"
	"strings"
)

// StreamParser defines the contract for all future format parsers
type StreamParser interface {
	Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error
}

// ParserRegistry maps lowercase file extensions (e.g., ".csv") to their parser implementations
var ParserRegistry = make(map[string]StreamParser)

// RegisterParser allows new formats to register themselves cleanly
func RegisterParser(ext string, parser StreamParser) {
	ParserRegistry[strings.ToLower(ext)] = parser
}
