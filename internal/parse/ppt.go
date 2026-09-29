package parse

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf16"
)

type PPTParser struct{}

func init() {
	RegisterParser(".ppt", &PPTParser{})
}

// OLE2, just as all other legacy documents, the emthod of extraction is the same
type pptOleHeader struct {
	Magic [8]byte
	_     [504]byte
}

func (p *PPTParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	rawBytes, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if len(rawBytes) < 512 {
		return errors.New("file size too small for legacy OLE2 presentation layout")
	}

	var header pptOleHeader
	if err := binary.Read(bytes.NewReader(rawBytes[:512]), binary.LittleEndian, &header); err != nil {
		return err
	}

	expectedMagic := [8]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
	if header.Magic != expectedMagic {
		return FallbackStringsParse(bytes.NewReader(rawBytes), filePath, resultsChan)
	}

	p.extractPPTTextStreams(rawBytes, filePath, resultsChan)
	return nil
}

func (p *PPTParser) extractPPTTextStreams(data []byte, filePath string, resultsChan chan<- JobResult) {
	i := 512
	n := len(data)

	var asciiBuf bytes.Buffer
	var u16Buf []uint16

	for i < n {
		b := data[i]

		if b >= 32 && b <= 126 {
			asciiBuf.WriteByte(b)
		} else {
			if asciiBuf.Len() >= 5 {
				p.scanAndSubmit(asciiBuf.String(), filePath, resultsChan)
			}
			asciiBuf.Reset()
		}

		if i+1 < n {
			val := binary.LittleEndian.Uint16(data[i : i+2])
			if (val >= 32 && val <= 126) || (val >= 0x0400 && val <= 0x04FF) {
				u16Buf = append(u16Buf, val)
				i += 2
				continue
			}
		}

		if len(u16Buf) >= 5 {
			decodedStr := string(utf16.Decode(u16Buf))
			p.scanAndSubmit(decodedStr, filePath, resultsChan)
		}
		u16Buf = u16Buf[:0]
		i++
	}

	if asciiBuf.Len() >= 5 {
		p.scanAndSubmit(asciiBuf.String(), filePath, resultsChan)
	}
	if len(u16Buf) >= 5 {
		decodedStr := string(utf16.Decode(u16Buf))
		p.scanAndSubmit(decodedStr, filePath, resultsChan)
	}
}

func (p *PPTParser) scanAndSubmit(text string, filePath string, resultsChan chan<- JobResult) {
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
