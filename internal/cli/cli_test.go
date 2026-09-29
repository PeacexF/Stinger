package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PeacexF/Stinger/internal/checkpoint"
	"github.com/PeacexF/Stinger/internal/config"
	"github.com/PeacexF/Stinger/internal/output"
	"github.com/PeacexF/Stinger/internal/resolver"
	"github.com/PeacexF/Stinger/internal/smtp"
	"github.com/PeacexF/Stinger/internal/verify"
)

func TestReadEmails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "emails.txt")
	os.WriteFile(p, []byte("# comment\nA@b.com\n\n  a@B.com \nc@d.com\n"), 0o644)
	got, err := readEmails(p)
	if err != nil || !slices.Equal(got, []string{"a@b.com", "c@d.com"}) {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestInterleaveByDomain(t *testing.T) {
	in := []string{"1@a", "2@a", "3@a", "1@b", "1@c", "2@b"}
	want := []string{"1@a", "1@b", "1@c", "2@a", "2@b", "3@a"}
	if got := interleaveByDomain(in); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func fakeVerifier(t *testing.T) {
	t.Helper()
	orig := newVerifier
	t.Cleanup(func() { newVerifier = orig })
	newVerifier = func(cfg *config.Config) *verify.Verifier {
		cache := resolver.NewCache(func(ctx context.Context, domain string) ([]string, error) {
			if domain == "nomx.test" {
				return []string{}, nil
			}
			return []string{"mx." + domain}, nil
		}, time.Hour, time.Hour)
		return verify.New(cfg, cache, func(ctx context.Context, job smtp.Job) smtp.Result {
			if strings.HasPrefix(job.Email, "good") {
				return smtp.Result{SMTPCode: 250}
			}
			return smtp.Result{SMTPCode: 550}
		})
	}
}

func TestVerifyAll_EndToEnd(t *testing.T) {
	fakeVerifier(t)
	cfg := config.Default()
	cfg.Output.OutputDir = t.TempDir()

	emails := []string{"good1@a.test", "bad@a.test", "good2@b.test", "x@nomx.test", "malformed"}
	if err := verifyAll(emails, "emails.txt", &cfg, ""); err != nil {
		t.Fatal(err)
	}
	s, err := output.Summarise(filepath.Join(cfg.Output.OutputDir, cfg.Output.FullJSONL))
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != 5 || s.Counts["valid"] != 2 || s.Counts["invalid"] != 3 {
		t.Errorf("summary = %+v", s)
	}
	valid, _ := os.ReadFile(filepath.Join(cfg.Output.OutputDir, cfg.Output.ValidTXT))
	if got := strings.Fields(string(valid)); len(got) != 2 {
		t.Errorf("valid = %v", got)
	}
	if _, err := os.Stat(filepath.Join(cfg.Output.OutputDir, checkpoint.Filename)); !os.IsNotExist(err) {
		t.Error("checkpoint should be removed after a clean run")
	}
}

func TestVerifyAll_Resume(t *testing.T) {
	fakeVerifier(t)
	cfg := config.Default()
	cfg.Output.OutputDir = t.TempDir()
	cfg.Logging.ShowProgress = false

	// A previous run finished good1 and was interrupted
	cp := checkpoint.New(cfg.Output.OutputDir, "emails.txt", 3)
	cp.Mark("good1@a.test")
	if err := cp.Save(); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(cfg.Output.OutputDir, cfg.Output.FullJSONL)
	os.WriteFile(jsonl, []byte(`{"email":"good1@a.test","status":"valid"}`+"\n"), 0o644)

	emails := []string{"good1@a.test", "good2@a.test", "bad@a.test"}
	if err := verifyAll(emails, "emails.txt", &cfg, cp.Path); err != nil {
		t.Fatal(err)
	}
	s, _ := output.Summarise(jsonl)
	if s.Total != 3 || s.Counts["valid"] != 2 {
		t.Errorf("resume should append the 2 remaining results: %+v", s)
	}
}
