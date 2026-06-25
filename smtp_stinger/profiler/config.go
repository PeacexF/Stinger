package profiler

import (
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Enabled bool

	// Directory where profile files will be written
	OutputDir string

	// Enabled profile names
	Profiles []string
}

func DefaultConfig() Config {
	return Config{
		Enabled:   false,
		OutputDir: "profiles",
		Profiles: []string{
			"cpu",
			"heap",
			"allocs",
			"trace",
			"goroutine",
			"mutex",
			"block",
			"threadcreate",
		},
	}
}

// ConfigFromEnv reads STINGER_PROFILE
//
// Examples:
//
//	STINGER_PROFILE=all
//	STINGER_PROFILE=cpu
//	STINGER_PROFILE=cpu,heap
//	STINGER_PROFILE=cpu,heap,trace
//	STINGER_PROFILE=off
func ConfigFromEnv() Config {
	cfg := DefaultConfig()

	value := strings.TrimSpace(strings.ToLower(os.Getenv("STINGER_PROFILE")))
	if value == "" || value == "0" || value == "off" {
		return cfg
	}

	cfg.Enabled = true

	if value == "1" || value == "all" {
		return cfg
	}

	cfg.Profiles = nil

	seen := make(map[string]struct{})

	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}

		if _, exists := seen[name]; exists {
			continue
		}

		seen[name] = struct{}{}
		cfg.Profiles = append(cfg.Profiles, name)
	}

	return cfg
}

func (c Config) Path(name string) string {
	return filepath.Join(c.OutputDir, name)
}
