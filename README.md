# SMTP-Stinger

**Bulk email verification over SMTP**

Stinger checks whether mailboxes exist by asking their mail servers directly. It looks up the domain's MX records and opens an SMTP session. Then it runs `EHLO` -> `STARTTLS` -> `MAIL FROM` -> `RCPT TO` and hangs up before sending anything. It also detects catch-all domains and retries greylisting. It can pull addresses out of ~40 file formats before you start.

![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)
![CLI](https://img.shields.io/badge/interface-CLI-black)
![SMTP](https://img.shields.io/badge/protocol-SMTP-green)
![MIT License](https://img.shields.io/badge/License-MIT-black.svg)

```
  [████████████████░░░░░░░░░░░░░░]  54.3%  543/1000  ✓357 ✗198 ?45
```

---

## Contents

- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Commands](#commands)
- [How verification works](#how-verification-works)
- [Output](#output)
- [Configuration](#configuration)
- [DNS setup](#dns-setup)
- [Performance and tuning](#performance-and-tuning)
- [Development](#development)
- [Responsible use](#responsible-use)
- [Disclaimer](DISCLAIMER.md)
- [License](#license)

---

## Features

- **Real SMTP checks.** Every address gets a genuine `RCPT TO` against the domain's own MX servers. No third-party API is involved.
- **Catch-all detection.** Stinger probes each domain once with a random address. Domains that accept anything are labelled `catch_all` instead of `valid`.
- **Detailed classification.** Each result has a status (`valid`, `invalid`, `catch_all`, `unknown`) and a sub-status such as `mailbox_not_found`, `greylisted`, `spam_block` or `no_mx`.
- **Retries and MX fallback.** Temporary failures (421/450/451, network errors) move on to the next MX, then retry with exponential backoff.
- **Concurrency limits.** You set a global cap and a per-domain cap. Input is interleaved by domain so one large domain can't stall the run.
- **Interrupt and resume.** Ctrl+C writes a checkpoint, and `--resume` continues where the run stopped. Results are written line by line, so nothing is lost.
- **Email extraction.** `stinger parse` pulls and deduplicates addresses from documents, spreadsheets, mailboxes, databases and archives, and it runs in parallel.
- **DNS doctor.** `stinger doctor` checks your A, PTR and SPF records before you start, so you don't burn your IP's reputation on a bad setup.
- **Single static binary.** No runtime and no dependencies to install on the server.

---

## Requirements

- **Go 1.26+** to build.
- **A server that can make outbound connections on port 25.** Most residential ISPs block it, and so do many cloud providers (AWS, GCP, Azure) until you ask them to lift the block. A VPS from a provider that allows port 25 works best.
- **A domain you control**, with an A record, a matching PTR (reverse DNS) record and an SPF record. See [DNS setup](#dns-setup).

---

## Installation

```bash
git clone https://github.com/PeacexF/Stinger
cd Stinger
go install ./cmd/stinger          # installs to $(go env GOPATH)/bin
```

Or build a binary in place, optionally cross-compiled for your server:

```bash
go build -o stinger ./cmd/stinger
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o stinger ./cmd/stinger
```

---

## Quick start

```bash
# 1. Create config.yaml, then set smtp.helo_hostname and smtp.mail_from
stinger init

# 2. Check your DNS identity (A, PTR, SPF)
stinger doctor

# 3. Extract and deduplicate addresses from your source files -> emails.txt
stinger parse ./emails

# 4. Verify
stinger check

# 5. Review the run
stinger stats results/results.jsonl
```

Valid addresses end up in `results/valid_emails.txt`. Full per-address details are in `results/results.jsonl`.

---

## Commands

Run `stinger <command> --help` for the full flag list of any command.

### `stinger init`

Writes a commented `config.yaml` template. If the file already exists, it asks before overwriting.

```bash
stinger init                      # ./config.yaml
stinger init /path/to/cfg.yaml
```

### `stinger doctor`

Checks the identity you'll present to mail servers, using the same resolvers as a real run:

- an A record exists for `helo_hostname`
- the PTR (reverse DNS) of that IP points back to `helo_hostname`
- the A record matches this machine's public IP
- the `mail_from` domain has an SPF record that covers the IP

The command exits with status 1 if a critical check fails.

```bash
stinger doctor
stinger doctor -c /path/to/config.yaml
```

```
  ── A Record ──────────────────────────────────────
  ✓  [OK  ]  A record for mail.yourdomain.com
          mail.yourdomain.com → 203.0.113.7

  ── PTR / Reverse DNS ─────────────────────────────
  ✓  [OK  ]  PTR matches helo_hostname
          203.0.113.7 → mail.yourdomain.com
  ✓  [OK  ]  A record matches this machine's IP
          Both resolve to 203.0.113.7

  ── SPF Record ────────────────────────────────────
  ✓  [OK  ]  SPF record for yourdomain.com
          v=spf1 ip4:203.0.113.7 ~all
  ✓  [OK  ]  SPF covers server IP
          ip4:203.0.113.7 found or permissive policy present

  ── Summary ───────────────────────────────────────
  All checks passed
  6/6 checks passed
```

### `stinger parse`

Extracts email addresses from files, directories (searched recursively) and glob patterns. Addresses are lowercased, deduplicated and written to one list.

```bash
stinger parse ./emails                          # -> emails.txt
stinger parse a.csv b.xlsx --out list.txt
stinger parse './exports/*.json'
stinger parse new.csv --out list.txt --append   # merge into an existing list, deduplicated
stinger parse ./emails --workers 8              # more parallel parsers (default 4)
stinger parse ./emails --no-summary             # totals only
```

**Supported formats**

| Kind | Extensions |
|---|---|
| Text and data | `txt` `log` `csv` `tsv` `json` `jsonl` `yaml` `yml` `toml` `ini` `html` `htm` |
| Mail and contacts | `eml` `msg` `mbox` `ldif` `vcf` |
| Office | `doc` `docx` `xls` `xlsx` `ppt` `pptx` `odt` `ods` `odp` `pdf` |
| Databases | `sqlite` `sqlite3` `db` |
| Archives | `zip` `7z` `rar` `tar` `tar.gz` `tgz` `gz` `bz2` `xz`. Files inside are parsed by their own extension. |

```
  Parsed (2 file(s)):
    +    467 unique  /data/emails/hotmail.txt
    +  45330 unique  /data/emails/orders.csv

  Raw emails found   : 53224
  Duplicates removed : 7427
  Unique emails      : 45797

  ────────────────────────────────────────────────────
  -> emails.txt  (45797 emails)
  ────────────────────────────────────────────────────
```

### `stinger check`

Verifies a list of addresses, one per line. Blank lines and `#` comments are ignored, and duplicates are removed.

```bash
stinger check                             # uses input.emails_file from config
stinger check emails.txt
stinger check emails.txt --out ./run-2    # output directory
stinger check emails.txt --limit 50       # global concurrency
stinger check emails.txt --per-domain 1   # per-domain concurrency
stinger check emails.txt --dry-run        # count unique addresses, no connections
stinger check emails.txt --no-progress
stinger check emails.txt -c prod.yaml
```

Before the run starts, Stinger resolves `gmail.com` MX as a smoke test. If that fails, it refuses to start and asks you to fix your resolvers.

**Interrupting and resuming**

- **First Ctrl+C:** stops taking new addresses and waits for the checks already running.
- **Second Ctrl+C:** abandons those in-flight checks as well.

Either way, `checkpoint.jsonl` is written to the output directory. To continue the run:

```bash
stinger check emails.txt --resume results/checkpoint.jsonl
```

The resumed run skips completed addresses and appends to the existing result files. When a run finishes cleanly, the checkpoint is deleted.

```
  ════════════════════════════════════════════════════
  Finished in 87.3s  (11.5 emails/sec)

  ✓ valid      312
  ~ catch_all  45
  ✗ invalid    198
  ? unknown    45
  ! error      0

  → results/valid_emails.txt
  → results/results.jsonl
  ════════════════════════════════════════════════════
```

### `stinger stats`

Summarises a `results.jsonl` file: counts per status and sub-status, average probe time, catch-all domains and a sample of unknowns.

```bash
stinger stats results/results.jsonl
```

```
  Total checked   : 1000

  ✓ valid          312  ( 31.2%)  ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓
  ~ catch_all       45  (  4.5%)  ▓▓
  ✗ invalid        198  ( 19.8%)  ▓▓▓▓▓▓▓▓▓
  ? unknown         45  (  4.5%)  ▓▓
  ! error            0  (  0.0%)

  Avg duration    : 342.0 ms/email

  Sub-status breakdown:
  invalid
    mailbox_not_found          171  ( 17.1%)
    no_mx                       27  (  2.7%)
  unknown
    greylisted                  30  (  3.0%)
    connect_failed              15  (  1.5%)
```

---

## How verification works

```
address ──► syntax check ──► MX lookup ──► catch-all probe (once per domain, cached)
                                                  │
                                                  ▼
                            RCPT TO on each MX in preference order
                                                  │
                  ┌───────────────────────────────┼──────────────────────────┐
               250/251                    5xx (not 503)            4xx / 503 / network error
                  │                              │                           │
        valid (or catch_all)                 invalid                next MX, then backoff
                                                                    -> unknown when retries
                                                                      run out
```

1. **Syntax.** Malformed addresses (`a@@b.com`, `a@.b.com`) are marked `invalid / malformed` without any network traffic.
2. **MX lookup.** Stinger queries your configured resolvers. Results are cached for `mx_cache_ttl`, and parallel lookups for the same domain share one query.
   - NXDOMAIN or no MX records -> `invalid / no_mx`.
   - DNS timeouts and server failures -> `unknown`.
3. **Catch-all probe.** Stinger sends `RCPT TO` for a random address on up to two MX hosts. If the server accepts it, the domain is marked catch-all and cached for `catch_all_cache_ttl`.
4. **Probe.** Stinger sends `EHLO` (falling back to `HELO`), then `STARTTLS` if offered and `try_tls` is on, then `MAIL FROM` and `RCPT TO`, and finally `QUIT`. It never sends `DATA`, so no email is delivered.
5. **Retry.** Up to `max_attempts` rounds over all MX hosts, waiting `backoff_base_sec × 2ⁿ` between rounds.

| Response | Action |
|---|---|
| 250, 251 | `valid` (or `catch_all`), stop |
| 5xx (except 503) | `invalid`, stop |
| 4xx, 503 | try next MX, then back off and retry |
| Connect or timeout error | try next MX, then back off and retry |

---

## Output

All files go to `output.output_dir` (default `./results`).

| File | Contents |
|---|---|
| `valid_emails.txt` | One address per line, for every `valid` and `catch_all` result |
| `results.jsonl` | One JSON object per checked address |
| `checkpoint.jsonl` | Only present after an interrupted run; pass it to `--resume` |

### `results.jsonl`

```json
{
  "email": "alice@example.com",
  "status": "valid",
  "sub_status": "confirmed",
  "smtp_code": 250,
  "smtp_message": "250 2.1.5 OK",
  "mx_used": "mx1.example.com",
  "tls_used": true,
  "is_catch_all_domain": false,
  "attempts": 1,
  "duration_ms": 312,
  "reason": "250 2.1.5 OK",
  "timestamp": "2026-01-15T10:23:45.123456+00:00"
}
```

`smtp_code`, `smtp_message` and `mx_used` are `null` when no SMTP conversation took place, for example after a syntax or DNS failure.

### Statuses

| Status | Sub-status | Meaning |
|---|---|---|
| `valid` | `confirmed` | Server accepted the recipient (250/251) |
| `catch_all` | `catch_all` | Accepted, but the domain accepts any address, so existence is not proven |
| `invalid` | `mailbox_not_found` | 550/551: user does not exist |
| | `mailbox_full` | 552: over quota |
| | `domain_rejected` | 553, generic 554 or other 5xx |
| | `spam_block` | 554 mentioning spam, policy, blocklists or reputation. **This is about your IP, not the address.** |
| | `syntax_error` | 501 |
| | `no_mx` | Domain doesn't exist or has no MX records |
| | `malformed` | Address failed the syntax check |
| `unknown` | `greylisted` | 451, still deferred after all retries |
| | `rate_limited` | 421 |
| | `mailbox_temp` | 450 |
| | `mailbox_full` | 452 |
| | `temp_failure` | Other 4xx or 503 |
| | `connect_failed` | TCP, TLS or protocol failure before `RCPT TO` |
| | `dns_timeout` / `dns_error` | MX lookup failed transiently |

`unknown` results are worth re-checking later. Greylisting and rate limits often clear within hours.

---

## Configuration

`stinger init` writes this file. Only the two `smtp` identity fields are required; every other key has the default shown here.

```yaml
smtp:
  helo_hostname: "mail.yourdomain.com"  # REQUIRED: needs A + PTR records
  mail_from: "verify@yourdomain.com"    # REQUIRED: domain needs SPF
  connect_timeout_sec: 10     # TCP connect timeout
  command_timeout_sec: 15     # timeout for each SMTP command / response
  port: 25
  try_tls: true               # use STARTTLS when offered

concurrency:
  global_limit: 100           # simultaneous SMTP connections in total
  per_domain_limit: 2         # simultaneous connections to any one domain

dns:
  mx_cache_ttl: 3600          # seconds
  catch_all_cache_ttl: 3600   # seconds
  resolvers: []               # empty -> 1.1.1.1, 8.8.8.8; "host" or "host:port"

retry:
  max_attempts: 3             # rounds over all MX hosts
  backoff_base_sec: 2         # wait 2s, 4s, 8s … between rounds

output:
  output_dir: "./results"
  valid_txt: "valid_emails.txt"
  full_jsonl: "results.jsonl"

input:
  emails_file: "./emails.txt" # used when `check` gets no file argument

logging:
  level: "INFO"               # DEBUG | INFO | WARN | ERROR (logs go to stderr)
  show_progress: true
```

The `--out`, `--limit`, `--per-domain` and `--no-progress` flags on `check` override the matching keys for one run.

---

## DNS setup

Mail servers judge you by the identity in `EHLO` and `MAIL FROM`. A missing or mismatched record leads to many more `554`, `spam_block` and timeout results.

| Record | Set it at | Example |
|---|---|---|
| A | Your DNS provider | `mail.yourdomain.com  A  203.0.113.7` |
| PTR (reverse DNS) | Your **VPS or hosting provider** panel, not your registrar | `203.0.113.7  PTR  mail.yourdomain.com` |
| SPF | Your DNS provider | `yourdomain.com  TXT  "v=spf1 ip4:203.0.113.7 ~all"` |

Using a dedicated domain is common practice. It doesn't need to receive mail; it only needs to pass these checks. Run `stinger doctor` after every change.

The **[full setup guide](setup-guide.md)** walks through A, PTR, SPF, DKIM and DMARC step by step.

---

## Performance and tuning

Throughput depends mostly on how fast remote servers respond, not on Stinger itself. As a rough guide:

| Volume | Typical time |
|---|---|
| 1,000 emails | 1–3 min |
| 10,000 emails | 10–30 min |

- **New ("cold") IP:** start low, for example `--limit 20 --per-domain 1`, and increase gradually. Large providers throttle and block aggressively.
- **Lots of `rate_limited` or `greylisted`:** lower `per_domain_limit`, or raise `max_attempts` and `backoff_base_sec`.
- **Lots of `connect_failed`:** check that outbound port 25 is open (`nc -vz gmail-smtp-in.l.google.com 25`).
- **Lots of `spam_block`:** your IP or domain reputation is the problem. Run `stinger doctor` and check blocklists before continuing.

---

## Development

```
cmd/stinger/        entry point
internal/
  cli/              commands (cobra)
  config/           config.yaml loading, defaults, template
  resolver/         DNS against explicit resolvers, MX and catch-all caches
  smtp/             a single RCPT TO probe
  verify/           classification, catch-all detection, retries, concurrency limits
  output/           result writers and the stats summary
  checkpoint/       interrupt and resume state
  doctor/           A / PTR / SPF / MX checks
  parse/            file collection, per-format parsers, deduplication
  profiler/         optional pprof / trace profiling
  ui/               terminal colours (disabled when not a TTY or when NO_COLOR is set)
```

```bash
go test -race ./...     # run the test suite
go vet ./...
gofmt -l .              # should print nothing
```

**Profiling `parse`:** pass `--profile`, or set `STINGER_PROFILE` to `all` or a list such as `cpu,heap,trace`. Profiles are written to `./profiles`.

**Adding a file format:** implement `parse.StreamParser` and call `parse.RegisterParser(".ext", &YourParser{})` from an `init()` function in `internal/parse`. `parse` then picks the extension up automatically, including inside archives.

---

## Responsible use

SMTP verification talks to other people's mail servers. Only verify lists you have a legitimate reason to hold, such as your own customers, sign-ups or CRM data. Respect rate limits, and follow applicable law (GDPR, CAN-SPAM and similar) and your hosting provider's acceptable use policy. Aggressive settings will get your IP blocklisted.

Read the full **[DISCLAIMER](DISCLAIMER.md)** before using Stinger.

---

## License

[MIT](LICENSE)
