// Package config loads, validates and scaffolds config.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultTemplate = `# ─────────────────────────────────────────────────────────────
#  SMTP-Stinger  —  config.yaml
#  Fill in the smtp section before running.
#  Run  stinger doctor  to validate your DNS setup.
# ─────────────────────────────────────────────────────────────

smtp:
  # Your sending identity. Must be a real domain you control.
  # Requirements:
  #   A record   : mail.yourdomain.com → YOUR.SERVER.IP
  #   PTR record : YOUR.SERVER.IP → mail.yourdomain.com  (set at your host/VPS panel)
  #   SPF record : yourdomain.com TXT "v=spf1 ip4:YOUR.SERVER.IP ~all"
  helo_hostname: ""         # e.g. "mail.yourdomain.com"
  mail_from: ""             # e.g. "stinger@yourdomain.com"

  connect_timeout_sec: 10   # TCP connect timeout
  command_timeout_sec: 15   # timeout for each SMTP command/response
  port: 25
  try_tls: true

concurrency:
  global_limit: 100         # max simultaneous SMTP connections
  per_domain_limit: 2       # max connections to any single domain

dns:
  mx_cache_ttl: 3600
  catch_all_cache_ttl: 3600
  resolvers: []
  # - "8.8.8.8"
  # - "1.1.1.1"

retry:
  max_attempts: 3
  backoff_base_sec: 2

output:
  output_dir: "./results"
  valid_txt: "valid_emails.txt"
  full_jsonl: "results.jsonl"

input:
  emails_file: "./emails.txt"

logging:
  level: "INFO"
  show_progress: true
`

type SMTP struct {
	HeloHostname      string `yaml:"helo_hostname"`
	MailFrom          string `yaml:"mail_from"`
	ConnectTimeoutSec int    `yaml:"connect_timeout_sec"`
	CommandTimeoutSec int    `yaml:"command_timeout_sec"`
	Port              int    `yaml:"port"`
	TryTLS            bool   `yaml:"try_tls"`
}

type Concurrency struct {
	GlobalLimit    int `yaml:"global_limit"`
	PerDomainLimit int `yaml:"per_domain_limit"`
}

type DNS struct {
	MXCacheTTL       int      `yaml:"mx_cache_ttl"`
	CatchAllCacheTTL int      `yaml:"catch_all_cache_ttl"`
	Resolvers        []string `yaml:"resolvers"`
}

type Retry struct {
	MaxAttempts    int     `yaml:"max_attempts"`
	BackoffBaseSec float64 `yaml:"backoff_base_sec"`
}

type Output struct {
	OutputDir string `yaml:"output_dir"`
	ValidTXT  string `yaml:"valid_txt"`
	FullJSONL string `yaml:"full_jsonl"`
}

type Input struct {
	EmailsFile string `yaml:"emails_file"`
}

type Logging struct {
	Level        string `yaml:"level"`
	ShowProgress bool   `yaml:"show_progress"`
}

type Config struct {
	SMTP        SMTP        `yaml:"smtp"`
	Concurrency Concurrency `yaml:"concurrency"`
	DNS         DNS         `yaml:"dns"`
	Retry       Retry       `yaml:"retry"`
	Output      Output      `yaml:"output"`
	Input       Input       `yaml:"input"`
	Logging     Logging     `yaml:"logging"`
}

// Default returns a Config with every optional field set.
func Default() Config {
	return Config{
		SMTP: SMTP{
			ConnectTimeoutSec: 10,
			CommandTimeoutSec: 15,
			Port:              25,
			TryTLS:            true,
		},
		Concurrency: Concurrency{GlobalLimit: 100, PerDomainLimit: 2},
		DNS:         DNS{MXCacheTTL: 3600, CatchAllCacheTTL: 3600},
		Retry:       Retry{MaxAttempts: 3, BackoffBaseSec: 2},
		Output: Output{
			OutputDir: "./results",
			ValidTXT:  "valid_emails.txt",
			FullJSONL: "results.jsonl",
		},
		Input:   Input{EmailsFile: "./emails.txt"},
		Logging: Logging{Level: "INFO", ShowProgress: true},
	}
}

// Scaffold writes the default template to dest.
func Scaffold(dest string) error {
	return os.WriteFile(dest, []byte(DefaultTemplate), 0o644)
}

// Load reads path, fills defaults for missing keys and, if requireSMTP is set,
// checks that helo_hostname and mail_from are present.
func Load(path string, requireSMTP bool) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("config file not found: %s\n  Run  stinger init  to create one", path)
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, fmt.Errorf("config file is empty: %s", path)
	}

	// yaml.v3 leaves fields untouched when the key is absent,
	// so decoding over Default() applies defaults
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	cfg.SMTP.HeloHostname = strings.TrimSpace(cfg.SMTP.HeloHostname)
	cfg.SMTP.MailFrom = strings.TrimSpace(cfg.SMTP.MailFrom)

	if requireSMTP && (cfg.SMTP.HeloHostname == "" || cfg.SMTP.MailFrom == "") {
		return nil, errors.New(
			"smtp.helo_hostname and smtp.mail_from must be set in config.yaml.\n" +
				"  Use a real domain with matching A, PTR, and SPF records.\n" +
				"  Run  stinger doctor  to validate your DNS setup")
	}
	if cfg.Concurrency.GlobalLimit < 1 {
		cfg.Concurrency.GlobalLimit = 1
	}
	if cfg.Concurrency.PerDomainLimit < 1 {
		cfg.Concurrency.PerDomainLimit = 1
	}
	if cfg.Retry.MaxAttempts < 1 {
		cfg.Retry.MaxAttempts = 1
	}
	return &cfg, nil
}
