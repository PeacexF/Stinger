package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/PeacexF/Stinger/internal/parse"
	"github.com/PeacexF/Stinger/internal/profiler"
	"github.com/PeacexF/Stinger/internal/ui"
)

func newParseCmd() *cobra.Command {
	var (
		out       string
		appendOut bool
		workers   int
		noSummary bool
		profile   bool
	)
	cmd := &cobra.Command{
		Use:   "parse SOURCE [SOURCE ...]",
		Short: "Extract and deduplicate emails from files, directories or globs",
		Long: `Extract and deduplicate emails from files, directories or globs.

SOURCE can be:
  a single file        stinger parse emails.csv
  multiple files       stinger parse a.csv b.txt
  a directory          stinger parse ./data         (walked recursively)
  a glob pattern       stinger parse './data/*.csv'

Supported: txt, csv, tsv, log, json(l), yaml, toml, ini, html, eml, msg, mbox,
ldif, vcf, doc(x), xls(x), ppt(x), odt/ods/odp, pdf, sqlite/db,
zip, 7z, rar, tar, tar.gz/tgz, gz, bz2, xz.

Files are parsed concurrently; deduplication happens in a single consumer
(FNV-64 hashing) that streams straight to the output file.`,
		Example: `  stinger parse leads.csv
  stinger parse ./data
  stinger parse ./data/*.csv --out clean.txt
  stinger parse a.csv b.csv --append --out master.txt
  stinger parse ./data --workers 8`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if profile {
				os.Setenv("STINGER_PROFILE", "all")
			}
			prof, err := profiler.Start()
			if err != nil {
				return fmt.Errorf("failed to start profiler: %w", err)
			}
			defer prof.Recover()
			defer func() {
				if err := prof.Stop(); err != nil {
					fmt.Fprintf(os.Stderr, "  failed to stop profiler: %v\n", err)
				}
			}()
			return runParse(args, out, appendOut, max(workers, 1), noSummary)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&out, "out", "o", "emails.txt", "output file for deduplicated emails")
	f.BoolVarP(&appendOut, "append", "a", false, "merge into the output file instead of overwriting")
	f.IntVarP(&workers, "workers", "w", 4, "number of parallel parsing workers")
	f.BoolVar(&noSummary, "no-summary", false, "suppress per-file breakdown, only show totals")
	f.BoolVar(&profile, "profile", false, "write CPU/heap/trace profiles to ./profiles")
	return cmd
}

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && !strings.HasPrefix(line, "#") {
			n++
		}
	}
	if sc.Err() != nil {
		return 0
	}
	return n
}

func runParse(sources []string, out string, appendOut bool, workers int, noSummary bool) error {
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
		return err
	}

	fmt.Printf("\n  %s\n", rule("─", 52))
	fmt.Println("  stinger parse")
	fmt.Printf("  %s\n", rule("─", 52))

	accepted, skipped := parse.CollectFiles(sources)
	// Never treat the output file itself as an input
	accepted = slices.DeleteFunc(accepted, func(p string) bool { return p == outAbs })

	if len(skipped) > 0 {
		fmt.Printf("\n  %s (%d, unsupported or not found):\n", ui.Yellow("Skipped"), len(skipped))
		for _, p := range skipped {
			fmt.Printf("    x %s\n", p)
		}
	}
	if len(accepted) == 0 {
		fmt.Print("\n  No supported files found to parse.\n\n")
		return exit(1)
	}

	// When appending, the existing output is parsed as one more input so
	// everything is deduplicated in a single pass
	existing := 0
	paths := accepted
	if appendOut {
		if _, err := os.Stat(outAbs); err == nil {
			existing = countLines(outAbs)
			paths = append([]string{outAbs}, accepted...)
		}
	}

	// Write to a temp file and rename, so the output can also be an input
	tmp, err := os.CreateTemp(filepath.Dir(outAbs), ".stinger-parse-*")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}

	res, err := parse.ParsePaths(paths, tmp.Name(), workers)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), outAbs); err != nil {
		return err
	}

	parsed := make(map[string]bool, len(res.FilesParsed))
	for _, f := range res.FilesParsed {
		parsed[f] = true
	}
	var empty []string
	for _, p := range accepted {
		if !parsed[p] {
			empty = append(empty, p)
		}
	}
	unique := res.TotalRaw - res.DuplicatesRemoved

	if !noSummary {
		files := slices.DeleteFunc(slices.Clone(res.FilesParsed), func(p string) bool { return p == outAbs })
		slices.Sort(files)
		fmt.Printf("\n  %s (%d file(s)):\n", ui.Green("Parsed"), len(files))
		for _, p := range files {
			fmt.Printf("    + %s  %s\n", ui.Cyan(fmt.Sprintf("%6d unique", res.PerFileUnique[p])), p)
		}
		if len(empty) > 0 {
			fmt.Printf("\n  %s (%d file(s)):\n", ui.Yellow("No emails found"), len(empty))
			for _, p := range empty {
				fmt.Printf("    - %s\n", p)
			}
		}
	}

	fmt.Println()
	fmt.Printf("  Raw emails found   : %d\n", res.TotalRaw)
	fmt.Printf("  Duplicates removed : %s\n", ui.Yellow(fmt.Sprint(res.DuplicatesRemoved)))
	fmt.Printf("  Unique emails      : %s\n", ui.Green(fmt.Sprint(unique)))

	if unique == 0 {
		fmt.Print("\n  No email addresses found in the provided files.\n\n")
		return exit(1)
	}

	fmt.Println()
	if appendOut && existing > 0 {
		fmt.Printf("  Merged with %d existing emails in %s\n", existing, out)
		fmt.Printf("  Final unique total  : %s\n", ui.Green(fmt.Sprint(unique)))
	}
	fmt.Printf("  %s\n", rule("─", 52))
	fmt.Printf("  -> %s  (%d emails)\n", ui.Cyan(out), unique)
	fmt.Printf("  %s\n\n", rule("─", 52))
	fmt.Printf("  Next:  stinger check %s\n\n", out)
	return nil
}
