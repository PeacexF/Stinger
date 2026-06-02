package stinger

import (
	"bufio"
	"os"
)

// reads messy txt line by line and pulls out all emails.
func ParseTXT(filePath string, resultsChan chan<- JobResult) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 64*1024) // 64KB line buffer
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
