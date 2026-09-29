package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte(content), 0o644)
	return p
}

func TestScaffold(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := Scaffold(p); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTP.HeloHostname != "" || cfg.SMTP.MailFrom != "" {
		t.Errorf("scaffold should leave smtp identity blank: %+v", cfg.SMTP)
	}
	// Template values must match the built-in defaults
	var raw Config
	b, _ := os.ReadFile(p)
	if err := yaml.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	raw.DNS.Resolvers = nil
	if want := Default(); raw.Concurrency != want.Concurrency || raw.Retry != want.Retry ||
		raw.Output != want.Output || raw.Logging != want.Logging || raw.SMTP != want.SMTP {
		t.Errorf("template drifted from Default():\n%+v\n%+v", raw, want)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml"), false); err == nil {
		t.Error("missing file: expected error")
	}
	if _, err := Load(write(t, "   \n"), false); err == nil {
		t.Error("empty file: expected error")
	}
	for _, c := range []string{
		"smtp:\n  mail_from: a@b.com\n",
		"smtp:\n  helo_hostname: mail.b.com\n",
		"smtp:\n  helo_hostname: '   '\n  mail_from: a@b.com\n",
	} {
		if _, err := Load(write(t, c), true); err == nil {
			t.Errorf("expected validation error for %q", c)
		}
	}
}

func TestLoadDefaultsAndOverrides(t *testing.T) {
	cfg, err := Load(write(t, "smtp:\n  helo_hostname: mail.b.com\n  mail_from: a@b.com\n"+
		"concurrency:\n  global_limit: 50\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTP.HeloHostname != "mail.b.com" || cfg.Concurrency.GlobalLimit != 50 {
		t.Errorf("explicit values lost: %+v", cfg)
	}
	if cfg.Concurrency.PerDomainLimit != 2 || cfg.DNS.MXCacheTTL != 3600 || cfg.Retry.MaxAttempts != 3 ||
		cfg.Output.OutputDir != "./results" || cfg.Logging.Level != "INFO" ||
		cfg.SMTP.ConnectTimeoutSec != 10 || cfg.SMTP.Port != 25 || !cfg.SMTP.TryTLS {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}
