package parse

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf16"
)

type DOCParser struct{}

func init() {
	RegisterParser(".doc", &DOCParser{})
}

type oleHeader struct {
	Magic           [8]byte
	_               [20]byte
	SectorShift     uint16
	MiniSectorShift uint16
	_               [6]byte
	NumSATSectors   uint32
	FirstSATSector  uint32
	_               [12]byte
	FirstMiniFATSeq uint32
	_               [4]byte
	FirstDirSector  uint32
}

func (p *DOCParser) Parse(r io.Reader, filePath string, resultsChan chan<- JobResult) error {
	rawBytes, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if len(rawBytes) < 512 {
		return errors.New("file too small for valid OLE2 structure")
	}

	var header oleHeader
	if err := binary.Read(bytes.NewReader(rawBytes[:512]), binary.LittleEndian, &header); err != nil {
		return err
	}

	// Verify  OLE2 signature: D0 CF 11 E0 A1 B1 1A E1
	expectedMagic := [8]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
	if header.Magic != expectedMagic {
		return FallbackStringsParse(bytes.NewReader(rawBytes), filePath, resultsChan)
	}

	sectorSize := 1 << header.SectorShift
	if sectorSize <= 0 || sectorSize > 4096 {
		return FallbackStringsParse(bytes.NewReader(rawBytes), filePath, resultsChan)
	}

	p.extractOLETextStreams(rawBytes, filePath, resultsChan)
	return nil
}

func (p *DOCParser) extractOLETextStreams(data []byte, filePath string, resultsChan chan<- JobResult) {
	// looks for printable ASCII or UTF-16 text chunks
	i := 512
	n := len(data)

	var asciiBuf bytes.Buffer
	var u16Buf []uint16

	for i < n {
		b := data[i]

		// Printable ASCII
		if b >= 32 && b <= 126 {
			asciiBuf.WriteByte(b)
		} else {
			if asciiBuf.Len() >= 5 {
				p.scanAndSubmit(asciiBuf.String(), filePath, resultsChan)
			}
			asciiBuf.Reset()
		}

		// check for 2-byte UTF-16LE characters
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

	// Flush buffers
	if asciiBuf.Len() >= 5 {
		p.scanAndSubmit(asciiBuf.String(), filePath, resultsChan)
	}
	if len(u16Buf) >= 5 {
		decodedStr := string(utf16.Decode(u16Buf))
		p.scanAndSubmit(decodedStr, filePath, resultsChan)
	}
}

func (p *DOCParser) scanAndSubmit(text string, filePath string, resultsChan chan<- JobResult) {
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
