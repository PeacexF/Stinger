package cli

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/PeacexF/Stinger/internal/checkpoint"
	"github.com/PeacexF/Stinger/internal/config"
	"github.com/PeacexF/Stinger/internal/output"
	"github.com/PeacexF/Stinger/internal/resolver"
	"github.com/PeacexF/Stinger/internal/smtp"
	"github.com/PeacexF/Stinger/internal/ui"
	"github.com/PeacexF/Stinger/internal/verify"
)

type checkOpts struct {
	cfgPath    string
	out        string
	limit      int
	perDomain  int
	noProgress bool
	dryRun     bool
	resume     string
}

func newCheckCmd() *cobra.Command {
	var o checkOpts
	cmd := &cobra.Command{
		Use:   "check [EMAILS_FILE]",
		Short: "Verify a list of email addresses via SMTP",
		Long: `Verify a list of email addresses via SMTP.

EMAILS_FILE defaults to the path set in config.yaml (input.emails_file).

Output files (written to results/ or --out):
  valid_emails.txt   — addresses that returned 250/251 (catch-all included)
  results.jsonl      — full data for every address checked
  checkpoint.jsonl   — only left behind by an interrupted run`,
		Example: `  stinger check
  stinger check emails.txt
  stinger check emails.txt --out ./my-results --limit 50
  stinger check emails.txt --dry-run
  stinger check --resume results/checkpoint.jsonl`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := ""
			if len(args) == 1 {
				file = args[0]
			}
			return runCheck(file, o)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.cfgPath, "config", "c", "config.yaml", "path to config.yaml")
	f.StringVarP(&o.out, "out", "o", "", "output directory (overrides config)")
	f.IntVarP(&o.limit, "limit", "l", 0, "global concurrency limit (overrides config)")
	f.IntVarP(&o.perDomain, "per-domain", "d", 0, "per-domain concurrency limit (overrides config)")
	f.BoolVar(&o.noProgress, "no-progress", false, "suppress live progress bar")
	f.BoolVar(&o.dryRun, "dry-run", false, "parse and deduplicate input, print count, then exit")
	f.StringVar(&o.resume, "resume", "", "resume from a checkpoint.jsonl left by an interrupted run")
	return cmd
}

// readEmails returns lowercased, deduplicated addresses in file order,
// skipping blank lines and # comments
func readEmails(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := make(map[string]struct{})
	var emails []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.ToLower(strings.TrimSpace(sc.Text()))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, dup := seen[line]; !dup {
			seen[line] = struct{}{}
			emails = append(emails, line)
		}
	}
	return emails, sc.Err()
}

// interleaveByDomain reorders emails round-robin across domains so a long run
// of one domain doesn't stall every worker on that domain's concurrency limit
func interleaveByDomain(emails []string) []string {
	groups := make(map[string][]string)
	var order []string
	for _, e := range emails {
		d := e[strings.LastIndexByte(e, '@')+1:]
		if _, ok := groups[d]; !ok {
			order = append(order, d)
		}
		groups[d] = append(groups[d], e)
	}
	out := make([]string, 0, len(emails))
	for len(out) < len(emails) {
		for _, d := range order {
			if g := groups[d]; len(g) > 0 {
				out = append(out, g[0])
				groups[d] = g[1:]
			}
		}
	}
	return out
}

func logLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}

func runCheck(emailsFile string, o checkOpts) error {
	cfg, err := config.Load(o.cfgPath, true)
	if err != nil {
		return err
	}
	if o.out != "" {
		cfg.Output.OutputDir = o.out
	}
	if o.limit > 0 {
		cfg.Concurrency.GlobalLimit = o.limit
	}
	if o.perDomain > 0 {
		cfg.Concurrency.PerDomainLimit = o.perDomain
	}
	if o.noProgress {
		cfg.Logging.ShowProgress = false
	}
	if emailsFile == "" {
		emailsFile = cfg.Input.EmailsFile
	}

	emails, err := readEmails(emailsFile)
	if os.IsNotExist(err) {
		return fmt.Errorf("emails file not found: %s", emailsFile)
	}
	if err != nil {
		return err
	}
	if len(emails) == 0 {
		fmt.Print("\n  No emails to process.\n\n")
		return nil
	}
	if o.dryRun {
		fmt.Printf("\n  Dry run: %d unique emails in %s\n", len(emails), emailsFile)
		fmt.Print("  (no SMTP connections made)\n\n")
		return nil
	}

	printBanner()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr,
		&slog.HandlerOptions{Level: logLevel(cfg.Logging.Level)})))

	fmt.Printf("  %s\n", rule("─", 52))
	fmt.Printf("  emails       : %d\n", len(emails))
	fmt.Printf("  helo         : %s\n", cfg.SMTP.HeloHostname)
	fmt.Printf("  mail_from    : %s\n", cfg.SMTP.MailFrom)
	fmt.Printf("  concurrency  : %d global / %d per domain\n",
		cfg.Concurrency.GlobalLimit, cfg.Concurrency.PerDomainLimit)
	fmt.Printf("  retries      : %d max attempts\n", cfg.Retry.MaxAttempts)
	fmt.Printf("  output       : %s\n", cfg.Output.OutputDir)
	fmt.Printf("  %s\n\n", rule("─", 52))

	return verifyAll(emails, emailsFile, cfg, o.resume)
}

// newVerifier builds the production verifier; tests swap it for a fake
var newVerifier = func(cfg *config.Config) *verify.Verifier {
	dnsResolver := resolver.New(cfg.DNS.Resolvers)
	cache := resolver.NewCache(
		func(ctx context.Context, domain string) ([]string, error) {
			return resolver.LookupMX(ctx, dnsResolver, domain)
		},
		time.Duration(cfg.DNS.MXCacheTTL)*time.Second,
		time.Duration(cfg.DNS.CatchAllCacheTTL)*time.Second,
	)
	return verify.New(cfg, cache, smtp.Probe)
}

func verifyAll(emails []string, emailsFile string, cfg *config.Config, resumePath string) error {
	outDir := cfg.Output.OutputDir
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	cp := checkpoint.New(outDir, emailsFile, len(emails))

	isResume := resumePath != ""
	if isResume {
		completed, err := checkpoint.Load(resumePath)
		if err != nil {
			return err
		}
		// Carry earlier progress forward so a second interruption still
		// records everything done so far
		remaining := emails[:0:0]
		for _, e := range emails {
			if _, done := completed[e]; done {
				cp.Mark(e)
			} else {
				remaining = append(remaining, e)
			}
		}
		fmt.Printf("  %s     : skipping %d already-completed emails\n",
			ui.Cyan("resume"), len(emails)-len(remaining))
		fmt.Printf("  remaining    : %d\n", len(remaining))
		fmt.Printf("  %s\n\n", rule("─", 52))
		emails = remaining
		if len(emails) == 0 {
			fmt.Print("  All emails already completed. Nothing to do.\n\n")
			return nil
		}
	}

	v := newVerifier(cfg)

	// Smoke test DNS before burning through the whole list
	if err := v.SmokeTestDNS(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "\n  %s: %v\n\n", ui.Red("ERROR"), err)
		fmt.Fprintln(os.Stderr, "  Hint: add explicit resolvers to config.yaml:")
		fmt.Fprintln(os.Stderr, "    dns:")
		fmt.Fprintln(os.Stderr, "      resolvers:")
		fmt.Fprintln(os.Stderr, `        - "1.1.1.1"`)
		fmt.Fprint(os.Stderr, "        - \"8.8.8.8\"\n\n")
		return exit(1)
	}

	writer, err := output.Open(outDir, cfg.Output.ValidTXT, cfg.Output.FullJSONL, isResume, cp)
	if err != nil {
		return err
	}

	// First Ctrl+C stops dispatching and lets in-flight checks finish;
	// the second cancels in-flight checks too
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := make(chan struct{})
	var stopped atomic.Bool
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		if _, ok := <-sigs; !ok {
			return
		}
		stopped.Store(true)
		close(stop)
		fmt.Fprintf(os.Stderr, "\n\n  %s — waiting for in-flight checks to finish...\n"+
			"  Press Ctrl+C again to force quit (results so far will be saved).\n\n", ui.Yellow("Interrupted"))
		if _, ok := <-sigs; !ok {
			return
		}
		fmt.Fprintf(os.Stderr, "\n  %s — abandoning in-flight checks.\n\n", ui.Red("Force quit"))
		cancel()
	}()

	total := len(emails)
	var done atomic.Int64
	var writeErr error
	var writeErrOnce sync.Once

	jobs := make(chan string)
	var wg sync.WaitGroup
	// The verifier's semaphores bound real connections; extra workers keep
	// slots busy while others sit in retry backoff
	for range max(cfg.Concurrency.GlobalLimit*2, 4) {
		wg.Go(func() {
			for email := range jobs {
				res := v.Verify(ctx, email)
				if ctx.Err() != nil {
					continue // aborted mid-check: leave it for --resume
				}
				if err := writer.Write(res); err != nil {
					writeErrOnce.Do(func() { writeErr = err; stopped.Store(true); cancel() })
					continue
				}
				done.Add(1)
				slog.Debug("checked", "email", res.Email, "status", res.Status)
			}
		})
	}

	progressDone := make(chan struct{})
	progressExited := make(chan struct{})
	if cfg.Logging.ShowProgress {
		go func() {
			defer close(progressExited)
			t := time.NewTicker(200 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					printProgress(int(done.Load()), total, writer.Counters())
				case <-progressDone:
					printProgress(int(done.Load()), total, writer.Counters())
					fmt.Println()
					return
				}
			}
		}()
	}

	start := time.Now()
dispatch:
	for _, e := range interleaveByDomain(emails) {
		select {
		case jobs <- e:
		case <-stop:
			break dispatch
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	elapsed := time.Since(start)

	close(progressDone)
	if cfg.Logging.ShowProgress {
		<-progressExited
	}
	if err := writer.Close(); err != nil && writeErr == nil {
		writeErr = err
	}

	completed := int(done.Load())
	interrupted := stopped.Load() || ctx.Err() != nil

	if interrupted {
		if err := cp.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: saving checkpoint: %v\n", ui.Red("ERROR"), err)
		}
		fmt.Printf("\n  %s\n", ui.Yellow(rule("═", 52)))
		fmt.Printf("  Interrupted after %.1fs  (%d/%d processed)\n\n", elapsed.Seconds(), completed, total)
		fmt.Printf("  Checkpoint saved → %s\n", cp.Path)
		fmt.Println("  Resume with:")
		fmt.Printf("    stinger check %s --resume %s\n\n", emailsFile, cp.Path)
	} else {
		cp.Delete()
		rate := 0.0
		if elapsed > 0 {
			rate = float64(completed) / elapsed.Seconds()
		}
		fmt.Printf("\n  %s\n", rule("═", 52))
		fmt.Printf("  Finished in %.1fs  (%.1f emails/sec)\n\n", elapsed.Seconds(), rate)
	}

	c := writer.Counters()
	fmt.Printf("  %s %d\n", ui.Green("✓ valid     "), c[verify.Valid])
	fmt.Printf("  %s %d\n", ui.Cyan("~ catch_all "), c[verify.CatchAll])
	fmt.Printf("  %s %d\n", ui.Red("✗ invalid   "), c[verify.Invalid])
	fmt.Printf("  %s %d\n", ui.Yellow("? unknown   "), c[verify.Unknown])
	fmt.Printf("  %s %d\n", ui.Magenta("! error     "), c[verify.Error])
	fmt.Printf("\n  → %s\n", writer.ValidPath)
	fmt.Printf("  → %s\n", writer.JSONLPath)
	fmt.Printf("  %s\n\n", rule("═", 52))

	if writeErr != nil {
		return fmt.Errorf("writing results: %w", writeErr)
	}
	return nil
}

func printProgress(done, total int, c map[verify.Status]int) {
	const width = 30
	filled, p := 0, 0.0
	if total > 0 {
		filled = width * done / total
		p = float64(done) / float64(total) * 100
	}
	bar := rule("█", filled) + rule("░", width-filled)
	fmt.Printf("\r  [%s] %5.1f%%  %d/%d  ✓%d ✗%d ?%d", bar, p, done, total,
		c[verify.Valid]+c[verify.CatchAll], c[verify.Invalid], c[verify.Unknown]+c[verify.Error])
}
