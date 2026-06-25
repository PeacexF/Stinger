package tests

// WONT COMPILE

import (
	"os"
	"path/filepath"
	"testing"

	parse "github.com/PeacexF/Stinger/smtp_stinger/parse"
)

func TestParseTXT(t *testing.T) {
	tests := []struct {
		name           string
		txtContent     string
		expectedEmails []string
	}{
		{
			name: "Standard Plaintext Parsing",
			txtContent: "Hello admin@target.com, please find the logs attached.\n" +
				"Contact support@stinger.dev if you have errors.\n",
			expectedEmails: []string{"admin@target.com", "support@stinger.dev"},
		},
		{
			name:           "Handles Lines with No Emails",
			txtContent:     "This is a sample text line containing nothing important.\n\nThird line here.",
			expectedEmails: nil,
		},
		{
			name:           "Extracts Multiple Entries per Single Line",
			txtContent:     "info@corp.ru,sales@corp.ru;billing@corp.ru",
			expectedEmails: []string{"info@corp.ru", "sales@corp.ru", "billing@corp.ru"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpFilePath := filepath.Join(tmpDir, "messy_source.txt")

			err := os.WriteFile(tmpFilePath, []byte(tt.txtContent), 0644)
			if err != nil {
				t.Fatalf("failed to create fixture file: %v", err)
			}

			resultsChan := make(chan parse.JobResult, 10)

			err = parse.Parse(tmpFilePath, resultsChan)
			if err != nil {
				t.Fatalf("ParseTXT returned an unexpected error: %v", err)
			}
			close(resultsChan)

			var actualEmails []string
			for res := range resultsChan {
				if res.FilePath != tmpFilePath {
					t.Errorf("expected FilePath %q, got %q", tmpFilePath, res.FilePath)
				}
				actualEmails = append(actualEmails, res.Email)
			}

			if len(actualEmails) != len(tt.expectedEmails) {
				t.Fatalf("expected %d emails, got %d: %v", len(tt.expectedEmails), len(actualEmails), actualEmails)
			}

			for i, email := range actualEmails {
				if email != tt.expectedEmails[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tt.expectedEmails[i], email)
				}
			}
		})
	}
}
