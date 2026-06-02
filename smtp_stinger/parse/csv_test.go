package stinger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCSV(t *testing.T) {
	tests := []struct {
		name           string
		csvContent     string
		expectedEmails []string
	}{
		{
			name: "Standard Clean CSV Parsing",
			csvContent: "id,name,email\n" +
				"1,Alex,alex@stinger.com\n" +
				"2,Bob,bob@victim.ru\n",
			expectedEmails: []string{"alex@stinger.com", "bob@victim.ru"},
		},
		{
			name:           "Skips Short Cells and Garbage",
			csvContent:     "short,a@b,no_at_sign,valid@target.org",
			expectedEmails: []string{"valid@target.org"},
		},
		{
			name:           "Extracts Multiple Emails From One Cell",
			csvContent:     "1,admin@corp.com;billing@corp.com,active",
			expectedEmails: []string{"admin@corp.com", "billing@corp.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpFilePath := filepath.Join(tmpDir, "test_data.csv")

			err := os.WriteFile(tmpFilePath, []byte(tt.csvContent), 0644)
			if err != nil {
				t.Fatalf("failed to create fixture file: %v", err)
			}

			resultsChan := make(chan JobResult, 10)

			err = ParseCSV(tmpFilePath, resultsChan)
			if err != nil {
				t.Fatalf("ParseCSV returned an unexpected error: %v", err)
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
