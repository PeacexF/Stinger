// Package cli implements the stinger command-line interface.
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/PeacexF/Stinger/internal/ui"
)

// Version is overridable at build time with -ldflags "-X .../internal/cli.Version=..."
var Version = "2.0.0"

const banner = `
  ███████╗████████╗██╗███╗   ██╗ ██████╗ ███████╗██████╗
  ██╔════╝╚══██╔══╝██║████╗  ██║██╔════╝ ██╔════╝██╔══██╗
  ███████╗   ██║   ██║██╔██╗ ██║██║  ███╗█████╗  ██████╔╝
  ╚════██║   ██║   ██║██║╚██╗██║██║   ██║██╔══╝  ██╔══██╗
  ███████║   ██║   ██║██║ ╚████║╚██████╔╝███████╗██║  ██║
  ╚══════╝   ╚═╝   ╚═╝╚═╝  ╚═══╝ ╚═════╝ ╚══════╝╚═╝  ╚═╝
`

func printBanner() {
	fmt.Println(ui.Yellow(banner))
	fmt.Printf("  High-performance SMTP verifier  v%s\n\n", Version)
}

func rule(ch string, n int) string { return strings.Repeat(ch, n) }

// exitError ends the process with code without printing anything further
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func exit(code int) error { return exitError{code} }

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "stinger",
		Short:         "SMTP-Stinger — high-performance SMTP email verifier.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newInitCmd(), newDoctorCmd(), newCheckCmd(), newParseCmd(), newStatsCmd())
	return root
}

// Execute runs the CLI and returns the process exit code
func Execute() int {
	err := newRoot().Execute()
	if err == nil {
		return 0
	}
	if ee, ok := errors.AsType[exitError](err); ok {
		return ee.code
	}
	fmt.Fprintf(os.Stderr, "\n  [stinger] ERROR: %v\n\n", err)
	return 1
}
