# CLI module
# Entry point for all commands: init, build, doctor, check, parse, stats

from __future__ import annotations

import asyncio
import logging
import sys
import time
from pathlib import Path

import click

from . import __version__

BANNER = r"""
  ███████╗████████╗██╗███╗   ██╗ ██████╗ ███████╗██████╗
  ██╔════╝╚══██╔══╝██║████╗  ██║██╔════╝ ██╔════╝██╔══██╗
  ███████╗   ██║   ██║██╔██╗ ██║██║  ███╗█████╗  ██████╔╝
  ╚════██║   ██║   ██║██║╚██╗██║██║   ██║██╔══╝  ██╔══██╗
  ███████║   ██║   ██║██║ ╚████║╚██████╔╝███████╗██║  ██║
  ╚══════╝   ╚═╝   ╚═╝╚═╝  ╚═══╝ ╚═════╝ ╚══════╝╚═╝  ╚═╝
"""

def _echo_banner():
    click.echo(click.style(BANNER, fg="yellow"))
    click.echo(f"  High-performance SMTP verifier  v{__version__}\n")



@click.group()
@click.version_option(__version__, prog_name="stinger")
def cli():
    """SMTP-Stinger — high-performance SMTP email verifier."""
    pass


# init 

@cli.command()
@click.argument("dest", default="config.yaml", metavar="[CONFIG_PATH]")
def init(dest):
    """
    Scaffold a config.yaml in the current directory.

    \b
    Usage:
      stinger init                  # creates ./config.yaml
      stinger init /path/to/cfg.yaml
    """
    from .config import scaffold_config
    path = Path(dest)
    if path.exists():
        if not click.confirm(f"  {path} already exists. Overwrite?"):
            click.echo("  Aborted.")
            return
    scaffold_config(path)
    click.echo(f"\n  ✓ Created {path}")
    click.echo("  → Open it and fill in smtp.helo_hostname and smtp.mail_from")
    click.echo("  → Run  stinger doctor  to validate your DNS setup\n")


# build

@cli.command("build")
def build_cmd():
    """
    Compile the Go SMTP worker binary.

    \b
    Must be run once after installation (or after updating smtp_worker.go).
    Requires the Go toolchain: https://go.dev/dl/
    """
    click.echo()
    from .builder import build as do_build
    success = do_build(verbose=True)
    sys.exit(0 if success else 1)


# doctor 

@cli.command()
@click.option("--config", "-c", default="config.yaml", show_default=True,
              help="Path to config.yaml")
def doctor(config):
    """
    Validate your DNS setup (A, PTR, SPF records).

    \b
    Reads helo_hostname and mail_from from config.yaml and checks:
      • A record exists for helo_hostname
      • PTR (reverse DNS) matches helo_hostname
      • A record IP matches this machine's outbound IP
      • SPF record exists and includes this machine's IP

    Run this before your first stinger check.
    """
    from .config import load_config
    from .doctor import run_doctor

    cfg = load_config(config, require_smtp=True)
    helo = cfg["smtp"]["helo_hostname"]
    mail_from = cfg["smtp"]["mail_from"]
    ok = run_doctor(helo, mail_from, cfg)
    sys.exit(0 if ok else 1)


# check

@cli.command()
@click.argument("emails_file", default=None, required=False, metavar="[EMAILS_FILE]")
@click.option("--config", "-c", default="config.yaml", show_default=True,
              help="Path to config.yaml")
@click.option("--out", "-o", default=None, metavar="DIR",
              help="Output directory (overrides config)")
@click.option("--limit", "-l", default=None, type=int, metavar="N",
              help="Global concurrency limit (overrides config)")
@click.option("--per-domain", "-d", default=None, type=int, metavar="N",
              help="Per-domain concurrency limit (overrides config)")
@click.option("--no-progress", is_flag=True, default=False,
              help="Suppress live progress counter")
@click.option("--dry-run", is_flag=True, default=False,
              help="Parse and deduplicate input, print count, then exit")
def check(emails_file, config, out, limit, per_domain, no_progress, dry_run):
    """
    Verify a list of email addresses via SMTP.

    \b
    EMAILS_FILE defaults to the path set in config.yaml (input.emails_file).

    \b
    Output files (written to results/ or --out):
      valid_emails.txt   — addresses that returned 250/251
      results.jsonl      — full data for every address checked

    \b
    Examples:
      stinger check
      stinger check emails.txt
      stinger check emails.txt --out ./my-results --limit 50
      stinger check emails.txt --dry-run
    """
    from .config import load_config
    from .verifier import Verifier
    from .output import ResultWriter
    from .models import Status
    from .worker import WORKER_BINARY

    cfg = load_config(config, require_smtp=True)

    # CLI overrides
    if out:
        cfg["output"]["output_dir"] = out
    if limit:
        cfg["concurrency"]["global_limit"] = limit
    if per_domain:
        cfg["concurrency"]["per_domain_limit"] = per_domain
    if no_progress:
        cfg["logging"]["show_progress"] = False

    # Resolve emails file
    emails_path = Path(emails_file) if emails_file else Path(cfg["input"]["emails_file"])
    if not emails_path.exists():
        click.echo(f"\n  [stinger] ERROR: emails file not found: {emails_path}\n", err=True)
        sys.exit(1)

    # Check Go binary
    if not WORKER_BINARY.exists():
        click.echo(
            f"\n  [stinger] ERROR: smtp_worker binary not found.\n"
            f"  Run:  stinger build\n",
            err=True,
        )
        sys.exit(1)

    # Load and deduplicate emails
    raw_lines = emails_path.read_text(encoding="utf-8", errors="replace").splitlines()
    emails = list(dict.fromkeys(
        line.strip().lower()
        for line in raw_lines
        if line.strip() and not line.strip().startswith("#")
    ))

    if not emails:
        click.echo("\n  No emails to process.\n")
        return

    if dry_run:
        click.echo(f"\n  Dry run: {len(emails)} unique emails in {emails_path}")
        click.echo("  (no SMTP connections made)\n")
        return

    _echo_banner()

    # Configure logging
    log_level = getattr(logging, cfg["logging"].get("level", "INFO").upper(), logging.INFO)
    logging.basicConfig(
        level=log_level,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%H:%M:%S",
    )

    # Print run summary
    click.echo(f"  {'─' * 52}")
    click.echo(f"  emails       : {len(emails)}")
    click.echo(f"  helo         : {cfg['smtp']['helo_hostname']}")
    click.echo(f"  mail_from    : {cfg['smtp']['mail_from']}")
    click.echo(f"  concurrency  : {cfg['concurrency']['global_limit']} global / "
               f"{cfg['concurrency']['per_domain_limit']} per domain")
    click.echo(f"  retries      : {cfg['retry']['max_attempts']} max attempts")
    click.echo(f"  output       : {cfg['output']['output_dir']}")
    click.echo(f"  {'─' * 52}\n")

    asyncio.run(_run_check(emails, cfg))


async def _run_check(emails: list[str], cfg: dict):
    from .verifier import Verifier
    from .output import ResultWriter
    from .models import Status

    verifier = Verifier(cfg)

    # Smoke test DNS before burning through the whole list
    dns_err = await verifier.smoke_test_dns()
    if dns_err:
        click.echo(f"\n  {click.style('ERROR', fg='red')}: {dns_err}\n", err=True)
        click.echo("  Hint: add explicit resolvers to config.yaml:", err=True)
        click.echo("    dns:", err=True)
        click.echo("      resolvers:", err=True)
        click.echo("        - \"1.1.1.1\"", err=True)
        click.echo("        - \"8.8.8.8\"\n", err=True)
        sys.exit(1)

    verifier.total = len(emails)
    show_progress = cfg["logging"]["show_progress"]

    start = time.monotonic()

    with ResultWriter(cfg) as writer:
        async def process(email: str):
            result = await verifier.verify(email)
            await verifier.tick()
            writer.write(result)
            if show_progress:
                pct = verifier.done / verifier.total * 100
                _print_progress(verifier.done, verifier.total, pct, writer.counters)

        await asyncio.gather(*[process(e) for e in emails])

    elapsed = time.monotonic() - start
    rate = len(emails) / elapsed if elapsed > 0 else 0

    # Final newline after progress
    if show_progress:
        click.echo()

    # Results summary
    c = writer.counters
    click.echo(f"\n  {'═' * 52}")
    click.echo(f"  Finished in {elapsed:.1f}s  ({rate:.1f} emails/sec)\n")
    click.echo(f"  {click.style('✓ valid     ', fg='green')} {c.get('valid', 0)}")
    click.echo(f"  {click.style('~ catch_all ', fg='cyan')} {c.get('catch_all', 0)}")
    click.echo(f"  {click.style('✗ invalid   ', fg='red')} {c.get('invalid', 0)}")
    click.echo(f"  {click.style('? unknown   ', fg='yellow')} {c.get('unknown', 0)}")
    click.echo(f"  {click.style('! error     ', fg='magenta')} {c.get('error', 0)}")

    out_dir = cfg["output"]["output_dir"]
    valid_name = cfg["output"]["valid_txt"]
    jsonl_name = cfg["output"]["full_jsonl"]
    click.echo(f"\n  → {out_dir}/{valid_name}")
    click.echo(f"  → {out_dir}/{jsonl_name}")
    click.echo(f"  {'═' * 52}\n")


def _print_progress(done: int, total: int, pct: float, counters: dict):
    bar_width = 30
    filled = int(bar_width * done / total) if total else 0
    bar = "█" * filled + "░" * (bar_width - filled)
    v = counters.get("valid", 0) + counters.get("catch_all", 0)
    i = counters.get("invalid", 0)
    u = counters.get("unknown", 0) + counters.get("error", 0)
    line = f"\r  [{bar}] {pct:5.1f}%  {done}/{total}  ✓{v} ✗{i} ?{u}"
    click.echo(line, nl=False, err=False)



# parse

@cli.command()
@click.argument("sources", nargs=-1, required=True, metavar="SOURCE [SOURCE ...]")
@click.option("--out", "-o", default="emails.txt", show_default=True,
              help="Output file to write deduplicated emails to")
@click.option("--append", "-a", is_flag=True, default=False,
              help="Append to output file instead of overwriting")
@click.option("--workers", "-w", default=4, show_default=True, type=int,
              help="Number of parallel Go parsing workers")
@click.option("--no-summary", is_flag=True, default=False,
              help="Suppress per-file breakdown, only show totals")
def parse(sources, out, append, workers, no_summary):
    """
    Extract and deduplicate emails from .txt and .csv files (Go-powered).

    \b
    SOURCE can be:
      a single file        stinger parse emails.csv
      multiple files       stinger parse a.csv b.txt
      a directory          stinger parse ./data
      a glob pattern       stinger parse './data/*.csv'

    All .txt and .csv files are parsed concurrently by the Go worker pool.
    Deduplication is done in a single-threaded Go consumer (FNV-64 hashing).
    Output is written directly to disk by Go — no Python buffering.

    \b
    Examples:
      stinger parse leads.csv
      stinger parse ./data
      stinger parse ./data/*.csv --out clean.txt
      stinger parse a.csv b.csv --append --out master.txt
      stinger parse ./data --workers 8
    """
    from .parse_worker import run_parse, PARSE_BINARY

    if not PARSE_BINARY.exists():
        click.echo(
            f"\n  ERROR: parse_worker binary not found.\n"
            f"  Run:  stinger build\n",
            err=True,
        )
        sys.exit(1)

    out_path = Path(out)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    # Count existing emails for append reporting before Go overwrites the file
    existing_count = 0
    append_source = None
    if append and out_path.exists():
        existing_count = sum(
            1 for line in out_path.read_text(encoding="utf-8").splitlines()
            if line.strip() and not line.startswith("#")
        )
        append_source = out_path

    result = run_parse(
        sources=list(sources),
        output_path=out_path,
        workers=workers,
        append_to=append_source,
    )

    # Report
    click.echo(f"\n  {'─' * 52}")
    click.echo("  stinger parse")
    click.echo(f"  {'─' * 52}")

    if result.error and not result.files_parsed:
        click.echo(f"\n  {click.style('ERROR', fg='red')}: {result.error}\n")
        sys.exit(1)

    if result.files_skipped:
        click.echo(f"\n  {click.style('Skipped', fg='yellow')} ({len(result.files_skipped)} file(s)):")
        for p in result.files_skipped:
            click.echo(f"    x {p}")

    if not result.files_parsed:
        click.echo("\n  No supported files (.txt, .csv) found to parse.\n")
        sys.exit(1)

    if not no_summary:
        click.echo(f"\n  {click.style('Parsed', fg='green')} ({len(result.files_parsed)} file(s)):")
        for fpath, count in result.per_file_unique.items():
            label = click.style(f"{count:>6} unique", fg="cyan")
            click.echo(f"    + {label}  {fpath}")

    click.echo()
    click.echo(f"  Raw emails found   : {result.total_raw}")
    click.echo(f"  Duplicates removed : {click.style(str(result.duplicates_removed), fg='yellow')}")
    click.echo(f"  Unique emails      : {click.style(str(result.unique), fg='green')}")

    if result.unique == 0:
        click.echo("\n  No email addresses found in the provided files.\n")
        sys.exit(1)

    click.echo()
    if append and existing_count:
        click.echo(f"  Merged with {existing_count} existing emails in {out_path}")
        click.echo(f"  Final unique total  : {click.style(str(result.unique), fg='green')}")
    click.echo(f"  {'─' * 52}")
    click.echo(f"  -> {click.style(str(out_path), fg='cyan')}  ({result.unique} emails)")
    click.echo(f"  {'─' * 52}\n")
    click.echo(f"  Next:  stinger check {out_path}\n")

# stats

@cli.command()
@click.argument("jsonl_file", metavar="RESULTS_JSONL")
def stats(jsonl_file):
    """
    Summarise a previous run from its results.jsonl file.

    \b
    Example:
      stinger stats results/results.jsonl
    """
    from .output import summarise_jsonl

    path = Path(jsonl_file)
    if not path.exists():
        click.echo(f"\n  ERROR: File not found: {path}\n", err=True)
        sys.exit(1)

    s = summarise_jsonl(path)
    total = s["total"]
    counts = s["counts"]

    click.echo(f"\n  {'─' * 48}")
    click.echo(f"  SMTP-Stinger — Stats  ({path.name})")
    click.echo(f"  {'─' * 48}")
    click.echo(f"  Total checked   : {total}")
    click.echo()

    status_styles = {
        "valid":     ("✓", "green"),
        "catch_all": ("~", "cyan"),
        "invalid":   ("✗", "red"),
        "unknown":   ("?", "yellow"),
        "error":     ("!", "magenta"),
    }
    for status, (icon, color) in status_styles.items():
        n = counts.get(status, 0)
        pct = n / total * 100 if total else 0
        bar = "▓" * int(pct / 2)
        label = click.style(f"{icon} {status:<10}", fg=color)
        click.echo(f"  {label}  {n:>6}  ({pct:5.1f}%)  {bar}")

    click.echo()
    click.echo(f"  Avg duration    : {s['avg_duration_ms']} ms/email")

    if s["catch_all_domains"]:
        click.echo(f"\n  Catch-all domains ({len(s['catch_all_domains'])}):")
        for d in s["catch_all_domains"][:20]:
            click.echo(f"    • {d}")
        if len(s["catch_all_domains"]) > 20:
            click.echo(f"    … and {len(s['catch_all_domains']) - 20} more")

    if s["sample_errors"]:
        click.echo(f"\n  Sample errors:")
        for e in s["sample_errors"]:
            click.echo(f"    {e}")

    click.echo(f"  {'─' * 48}\n")


def main():
    cli()