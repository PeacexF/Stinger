package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/PeacexF/Stinger/internal/config"
	"github.com/PeacexF/Stinger/internal/doctor"
	"github.com/PeacexF/Stinger/internal/output"
	"github.com/PeacexF/Stinger/internal/ui"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [CONFIG_PATH]",
		Short: "Scaffold a config.yaml in the current directory",
		Example: `  stinger init                  # creates ./config.yaml
  stinger init /path/to/cfg.yaml`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := "config.yaml"
			if len(args) == 1 {
				dest = args[0]
			}
			if _, err := os.Stat(dest); err == nil {
				fmt.Printf("  %s already exists. Overwrite? [y/N]: ", dest)
				answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
					fmt.Println("  Aborted.")
					return nil
				}
			}
			if err := config.Scaffold(dest); err != nil {
				return err
			}
			fmt.Printf("\n  ✓ Created %s\n", dest)
			fmt.Println("  → Open it and fill in smtp.helo_hostname and smtp.mail_from")
			fmt.Print("  → Run  stinger doctor  to validate your DNS setup\n\n")
			return nil
		},
	}
}

func newDoctorCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate your DNS setup (A, PTR, SPF records)",
		Long: `Validate your DNS setup (A, PTR, SPF records).

Reads helo_hostname and mail_from from config.yaml and checks:
  • A record exists for helo_hostname
  • PTR (reverse DNS) matches helo_hostname
  • A record IP matches this machine's outbound IP
  • SPF record exists and includes this machine's IP

Run this before your first stinger check.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath, true)
			if err != nil {
				return err
			}
			if !doctor.Run(cmd.Context(), os.Stdout, cfg.SMTP.HeloHostname, cfg.SMTP.MailFrom, cfg.DNS.Resolvers) {
				return exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.yaml", "path to config.yaml")
	return cmd
}

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "stats RESULTS_JSONL",
		Short:   "Summarise a previous run from its results.jsonl file",
		Example: "  stinger stats results/results.jsonl",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("file not found: %s", path)
			}
			s, err := output.Summarise(path)
			if err != nil {
				return err
			}
			printStats(filepath.Base(path), s)
			return nil
		},
	}
}

type statusStyle struct {
	name  string
	icon  string
	color func(string) string
}

var statusStyles = []statusStyle{
	{"valid", "✓", ui.Green},
	{"catch_all", "~", ui.Cyan},
	{"invalid", "✗", ui.Red},
	{"unknown", "?", ui.Yellow},
	{"error", "!", ui.Magenta},
}

var subGroups = []struct {
	name  string
	keys  []string
	color func(string) string
}{
	{"valid", []string{"confirmed"}, ui.Green},
	{"catch_all", []string{"catch_all"}, ui.Cyan},
	{"invalid", []string{"mailbox_not_found", "mailbox_full", "domain_rejected",
		"spam_block", "syntax_error", "no_mx", "malformed"}, ui.Red},
	{"unknown", []string{"greylisted", "rate_limited", "mailbox_temp",
		"connect_failed", "dns_timeout", "dns_error", "worker_error", "temp_failure"}, ui.Yellow},
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

func printStats(name string, s *output.Summary) {
	fmt.Printf("\n  %s\n", rule("─", 48))
	fmt.Printf("  SMTP-Stinger — Stats  (%s)\n", name)
	fmt.Printf("  %s\n", rule("─", 48))
	fmt.Printf("  Total checked   : %d\n\n", s.Total)

	for _, st := range statusStyles {
		n := s.Counts[st.name]
		p := pct(n, s.Total)
		label := st.color(fmt.Sprintf("%s %-10s", st.icon, st.name))
		fmt.Printf("  %s  %6d  (%5.1f%%)  %s\n", label, n, p, rule("▓", int(p/2)))
	}

	fmt.Printf("\n  Avg duration    : %.1f ms/email\n", s.AvgDurationMS)

	if len(s.SubCounts) > 0 {
		fmt.Print("\n  Sub-status breakdown:\n")
		for _, g := range subGroups {
			groupTotal := 0
			for _, k := range g.keys {
				groupTotal += s.SubCounts[k]
			}
			if groupTotal == 0 {
				continue
			}
			fmt.Printf("  %s\n", g.color(g.name))
			for _, k := range g.keys {
				if n := s.SubCounts[k]; n > 0 {
					fmt.Printf("    %-22s  %6d  (%5.1f%%)\n", k, n, pct(n, s.Total))
				}
			}
		}
	}

	if n := len(s.CatchAllDomains); n > 0 {
		fmt.Printf("\n  Catch-all domains (%d):\n", n)
		for _, d := range s.CatchAllDomains[:min(20, n)] {
			fmt.Printf("    • %s\n", d)
		}
		if n > 20 {
			fmt.Printf("    … and %d more\n", n-20)
		}
	}

	if len(s.SampleErrors) > 0 {
		fmt.Print("\n  Sample unknowns/errors:\n")
		for _, e := range s.SampleErrors {
			fmt.Printf("    %s\n", e)
		}
	}
	fmt.Printf("  %s\n\n", rule("─", 48))
}
