package parse

import (
	"bufio"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
)

type EMLParser struct{}

func init() {
	RegisterParser(".eml", &EMLParser{})
}

func (p *EMLParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	// Parse the email headers
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return FallbackStringsParse(r, filePath, resultsChan)
	}

	// Process standard headers
	headerFields := []string{"From", "To", "Cc", "Reply-To"}
	for _, field := range headerFields {
		val := msg.Header.Get(field)
		if val != "" {
			p.extractEmailsFromString(val, filePath, resultsChan)
		}
	}

	contentType := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// Default
		mediaType = "text/plain"
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary, ok := params["boundary"]
		if ok {
			p.parseMultipartStream(msg.Body, boundary, filePath, resultsChan)
		}
	} else {
		p.parseTextStream(msg.Body, filePath, resultsChan)
	}

	return nil
}

// recursively unrolls MIME
func (p *EMLParser) parseMultipartStream(r io.Reader, boundary string, filePath string, resultsChan chan<- JobResult) {
	mr := multipart.NewReader(r, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}

		partType, partParams, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))

		if strings.HasPrefix(partType, "multipart/") {
			nestedBoundary, ok := partParams["boundary"]
			if ok {
				p.parseMultipartStream(part, nestedBoundary, filePath, resultsChan)
			}
		} else {
			// Extract plain text blocks
			p.parseTextStream(part, filePath, resultsChan)
		}
	}
}

func (p *EMLParser) parseTextStream(r io.Reader, filePath string, resultsChan chan<- JobResult) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) >= 5 && EmailRegex.MatchString(line) {
			p.extractEmailsFromString(line, filePath, resultsChan)
		}
	}
}

func (p *EMLParser) extractEmailsFromString(input string, filePath string, resultsChan chan<- JobResult) {
	matches := EmailRegex.FindAllString(input, -1)
	for _, email := range matches {
		resultsChan <- JobResult{
			FilePath: filePath,
			Email:    email,
		}
	}
}
