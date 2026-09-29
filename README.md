# SMTP-Stinger

High-performance SMTP email verifier, shipped as a single Go binary.  

![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)
![YAML](https://img.shields.io/badge/YAML-CB171E?logo=yaml&logoColor=white)  
![CLI](https://img.shields.io/badge/interface-CLI-black)
![MIT License](https://img.shields.io/badge/License-MIT-black.svg)  
![SMTP](https://img.shields.io/badge/protocol-SMTP-green)
![Email Verification](https://img.shields.io/badge/email-verification-success)
![Email Parsing](https://img.shields.io/badge/email-parsing-green)
![Catch-All Detection](https://img.shields.io/badge/catch--all-detection-green)


---

## Installation

```bash
git clone https://github.com/PeacexF/Stinger
cd Stinger

# Build and install the binary (requires Go 1.26+) into $(go env GOPATH)/bin
go install ./cmd/stinger
# or build it in place:  go build -o stinger ./cmd/stinger

# Create config
stinger init

# Fill in helo_hostname and mail_from in config.yaml, then validate
stinger doctor

# Parse your source files into the `emails_file` from config
# Put every file that contains emails into a directory first (see `stinger parse` for formats)
stinger parse ./emails

# Verify
stinger check
```

---

## Commands

### `stinger init`
make a `config.yaml` in the current directory.

```bash
stinger init                      # → ./config.yaml
stinger init /path/to/cfg.yaml    # custom location
```

---

### `stinger doctor`
Validate your DNS setup before running any checks. Tests A record, PTR/reverse DNS, SPF, and IP consistency.

```bash
stinger doctor
stinger doctor --config /path/to/config.yaml
```

Example output:
```
  ── A Record ──────────────────────────────────────
  ✓  [OK]  A record for mail.yourdomain.com
             mail.yourdomain.com → 1.2.3.4

  ── PTR / Reverse DNS ─────────────────────────────
  ✓  [OK]  PTR matches helo_hostname
             1.2.3.4 → mail.yourdomain.com
  ✓  [OK]  A record matches this machine's IP
             Both resolve to 1.2.3.4

  ── SPF Record ────────────────────────────────────
  ✓  [OK]  SPF record for yourdomain.com
             v=spf1 ip4:1.2.3.4 ~all
  ✓  [OK]  SPF includes this machine's IP
             ip4:1.2.3.4 found or permissive policy present

  ── Summary ───────────────────────────────────────
  All checks passed
  5/5 checks passed
```

---

### `stinger parse`
Recommended usage:  
make a directory with all your email-filled files and point `parse` at it (directories are walked recursively).

Supported formats: txt, csv, tsv, log, json/jsonl, yaml, toml, ini, html, eml, msg, mbox, ldif, vcf,
doc/docx, xls/xlsx, ppt/pptx, odt/ods/odp, pdf, sqlite/db, and archives (zip, 7z, rar, tar, tar.gz/tgz, gz, bz2, xz).

```bash
stinger parse ./emails                          # → emails.txt
stinger parse ./emails --out list.txt
stinger parse new.csv --out list.txt --append   # merge + dedupe into an existing list
stinger parse ./emails --workers 8
stinger parse ./emails --profile                # write pprof/trace profiles to ./profiles
```

Example output:
```bash
  ────────────────────────────────────────────────────
  stinger parse
  ────────────────────────────────────────────────────

  Parsed (2 file(s)):
    +    467 unique  /Users/peace/Desktop/Dev/SMTP-Stinger/emails/hotmail.txt
    +  45330 unique  /Users/peace/Desktop/Dev/SMTP-Stinger/emails/orders.csv

  Raw emails found   : 53224
  Duplicates removed : 7427
  Unique emails      : 45797

  ────────────────────────────────────────────────────
  -> emails.txt  (45797 emails)
  ────────────────────────────────────────────────────
```

---

### `stinger check`
Run the verifier against a list of emails.

```bash
stinger check                           # uses emails_file from config.yaml
stinger check emails.txt                # specific file
stinger check emails.txt --out ./out    # custom output dir
stinger check emails.txt --limit 50     # override global concurrency
stinger check emails.txt --per-domain 1 # override per-domain limit
stinger check emails.txt --no-progress  # suppress progress bar
stinger check emails.txt --dry-run      # count + deduplicate, no SMTP
stinger check --config /path/cfg.yaml emails.txt
stinger check emails.txt --resume results/checkpoint.jsonl
```

Ctrl+C once stops dispatching and waits for in-flight checks; press it again to abandon them.
Either way a `checkpoint.jsonl` is written to the output directory, and `--resume` skips everything already checked.

Live progress bar during run:
```
  [████████████████░░░░░░░░░░░░░░]  54.3%  543/1000  ✓312 ✗198 ?33
```

Final summary:
```
  ════════════════════════════════════════════════════════
  Finished in 87.3s  (11.5 emails/sec)

  ✓ valid       312
  ~ catch_all    45
  ✗ invalid     198
  ? unknown      33
  ! error        12

  → results/valid_emails.txt
  → results/results.jsonl
  ════════════════════════════════════════════════════════
```

---

### `stinger stats`
Summarise a previous run from its `results.jsonl`.

```bash
stinger stats results/results.jsonl
```

Output:
```
  ────────────────────────────────────────────────
  SMTP-Stinger — Stats  (results.jsonl)
  ────────────────────────────────────────────────
  Total checked   : 1000

  ✓ valid          312  (31.2%)  ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓
  ~ catch_all       45   (4.5%)  ▓▓
  ✗ invalid        198  (19.8%)  ▓▓▓▓▓▓▓▓▓▓
  ? unknown         33   (3.3%)  ▓
  ! error           12   (1.2%)

  Avg duration    : 342 ms/email

  Catch-all domains (3):
    • gmail.com
    • yahoo.com
    • hotmail.com
  ────────────────────────────────────────────────
```

#### --help
for more info on commands, run: `stinger [cmd] --help`

---

## Project structure

```
├── cmd/stinger/            main package (the `stinger` binary)
├── internal/
│   ├── cli/                cobra commands: init, doctor, check, parse, stats
│   ├── config/             config.yaml loading, defaults, template
│   ├── resolver/           DNS against explicit resolvers + MX / catch-all caches
│   ├── smtp/               single RCPT TO probe (EHLO, STARTTLS, MAIL FROM, RCPT TO)
│   ├── verify/             statuses, classification, catch-all detection, retries, limits
│   ├── output/             valid_emails.txt / results.jsonl writer, stats summary
│   ├── checkpoint/         interrupt / resume state
│   ├── doctor/             A / PTR / SPF / MX checks
│   ├── parse/              concurrent email extraction for ~40 file formats + dedup
│   ├── profiler/           optional pprof / trace profiling (STINGER_PROFILE)
│   └── ui/                 terminal colours
├── config_EXAMPLE.yaml
└── go.mod
```

Run the tests with `go test -race ./...`.

---

## Configuration reference

```yaml
smtp:
  helo_hostname: "mail.yourdomain.com"  # REQUIRED — domain with A + PTR + SPF
  mail_from: "verify@yourdomain.com"    # REQUIRED — sender address
  connect_timeout_sec: 10     # TCP connect timeout
  command_timeout_sec: 15     # timeout for each SMTP command / response
  port: 25
  try_tls: true

concurrency:
  global_limit: 100       # total simultaneous SMTP connections
  per_domain_limit: 2     # max to any one domain (avoids hammering Gmail etc.)

dns:
  mx_cache_ttl: 3600          # seconds to cache MX records
  catch_all_cache_ttl: 3600   # seconds to cache catch-all probe results
  resolvers: []               # empty → 1.1.1.1 and 8.8.8.8

retry:
  max_attempts: 3             # total attempts per email (across all MX)
  backoff_base_sec: 2         # exponential backoff base

output:
  output_dir: "./results"
  valid_txt: "valid_emails.txt"
  full_jsonl: "results.jsonl"

input:
  emails_file: "./emails.txt"

logging:
  level: "INFO"               # DEBUG | INFO | WARNING | ERROR
  show_progress: true
```

---

## Output format

### `valid_emails.txt`
Plain list — one address per line — of every email that received a `250` or `251` response. Includes catch-all domains (they still confirmed the address existed via SMTP).

### `results.jsonl`
One JSON object per line for every email processed:

```json
{
  "email": "alice@example.com",
  "status": "valid",
  "sub_status": "confirmed",
  "smtp_code": 250,
  "smtp_message": "2.1.5 OK",
  "mx_used": "mx1.example.com",
  "tls_used": true,
  "is_catch_all_domain": false,
  "attempts": 1,
  "duration_ms": 312,
  "reason": "2.1.5 OK",
  "timestamp": "2025-01-15T10:23:45.123456+00:00"
}
```

| Status | Meaning |
|---|---|
| `valid` | 250/251, domain is not catch-all |
| `catch_all` | 250/251, but domain accepts anything |
| `invalid` | 550–554 permanent rejection |
| `unknown` | Temp failure / greylisted after all retries |
| `error` | Reserved; connection failures are reported as `unknown` / `connect_failed` |

---

## DNS setup guide

### Full setup guide: [link](setup-guide.md)  

Both `helo_hostname` and `mail_from` must use a real domain you control.
`stinger doctor` will tell you exactly what to fix, but here's the full picture:

| Record | Where to set it | Example |
|---|---|---|
| A record | Your DNS registrar / DNS provider | `mail.yourdomain.com A 1.2.3.4` |
| PTR record | Your VPS / hosting provider panel (not your registrar) | `1.2.3.4 PTR mail.yourdomain.com` |
| SPF record | Your DNS registrar / DNS provider | `yourdomain.com TXT "v=spf1 ip4:1.2.3.4 ~all"` |

A dedicated throwaway domain is common practice — it doesn't need to receive email, just pass DNS checks.

---

## Retry logic

| Response | Action |
|---|---|
| 250, 251 | Valid — stop |
| 550–554 | Invalid — stop |
| 421, 450, 451 | Retry (next MX, then backoff) |
| Network error | Retry next MX |
| NXDOMAIN | Invalid — stop |

---

## Performance

| Volume | Estimated time |
|---|---|
| 1,000 emails | ~1–3 min |
| 10,000 emails | ~10–30 min |

Actual speed depends on MX server latency and throttling. Tune `global_limit` and `per_domain_limit` based on your IP reputation.