package parse

import (
	"bufio"
	"io"
	"strings"

	"golang.org/x/net/html"
)

type HTMLParser struct{}

func init() {
	parser := &HTMLParser{}
	RegisterParser(".html", parser)
	RegisterParser(".htm", parser)
}

func (p *HTMLParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	tokenizer := html.NewTokenizer(r)

	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			err := tokenizer.Err()
			if err == io.EOF {
				break
			}
			return p.parseRawHTMLFallback(r, filePath, resultsChan)
		}

		switch tokenType {
		case html.TextToken, html.CommentToken:
			// plain scan
			text := string(tokenizer.Text())
			if len(text) >= 5 && EmailRegex.MatchString(text) {
				matches := EmailRegex.FindAllString(text, -1)
				for _, email := range matches {
					resultsChan <- JobResult{
						FilePath: filePath,
						Email:    email,
					}
				}
			}

		case html.StartTagToken, html.SelfClosingTagToken:
			// scan attributes inside elements
			token := tokenizer.Token()
			for _, attr := range token.Attr {
				val := attr.Val
				// Strip typical prefixes
				if strings.HasPrefix(strings.ToLower(val), "mailto:") {
					val = val[7:]
				}

				if len(val) >= 5 && EmailRegex.MatchString(val) {
					matches := EmailRegex.FindAllString(val, -1)
					for _, email := range matches {
						resultsChan <- JobResult{
							FilePath: filePath,
							Email:    email,
						}
					}
				}
			}
		}
	}
	return nil
}

func (p *HTMLParser) parseRawHTMLFallback(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) >= 5 && EmailRegex.MatchString(line) {
			matches := EmailRegex.FindAllString(line, -1)
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
