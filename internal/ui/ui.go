// Package ui provides ANSI colour helpers that switch off when stdout is
// not a terminal or NO_COLOR is set.
package ui

import "os"

var enabled = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

func style(code string) func(string) string {
	return func(s string) string {
		if !enabled {
			return s
		}
		return "\033[" + code + "m" + s + "\033[0m"
	}
}

var (
	Red     = style("31")
	Green   = style("32")
	Yellow  = style("33")
	Magenta = style("35")
	Cyan    = style("36")
)
