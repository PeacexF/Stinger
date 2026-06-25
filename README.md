# SMTP-Stinger

High-performance SMTP email verifier. Python async orchestration + Go low-level socket worker, packaged as a CLI tool.  

![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)
![Python](https://img.shields.io/badge/Python-3776AB?logo=python&logoColor=white)
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

# Install Python package
pip install -e .

# Compile the Go worker (requires Go)
stinger build

# Create config
stinger init

# Fill in helo_hostname and mail_from in config.yaml, then validate
stinger doctor

# Parse your list of emails into a list of `emails_file` in config
# Beforehand, make a dir in the Stinger/ with all the .csv and .txt files that contain emails that need to be checked
stinger parse ./emails/*
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

### `stinger build`
Compile the Go SMTP worker binary. Run once after install and after any update to `smtp_worker.go`.

```bash
stinger build
```
[Requires Go](https://go.dev/dl/)

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
make a directory with all your email-filled files (.csv and .txt supported)  

Run command:
```bash
stinger parse ./emails/*
```

All parsed emails are now in the **emails_file** from config

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
```

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
├── go.mod
├── pyproject.toml
├── requirements.txt
├── config.yaml             configuration, created by `stinger init`
├── emails/                 folder with the initial emails list (recommended)
│   ├── hotmail.txt
│   └── orders.csv
├── results/                
│   ├── results.jsonl       full data on the checked emails
│   └── valid_emails.txt    valid (250/251 code) checked emails
├── emails.txt              parsed, deduplicated list of emails
├── smtp_worker.go          smtp probe -> smtp_worker
├── smtp_stinger/
│   ├── __init__.py         version
│   ├── builder.py          build go binaries
│   ├── cli.py              cli module
│   ├── config.py           config creation
│   ├── dns_cache.py        DNS helpers and caching
│   ├── doctor.py           validation of records
│   ├── models.py           shared models
│   ├── output.py           output writters
│   ├── parse_worker.py     calls the parse_worker go binary
│   ├── verifier.py         core verifier
│   ├── worker.py           calls the smtp_worker go binary
│   ├── parse_worker        go binaries, both built by `stinger build`
│   ├── smtp_worker
│   ├── main.go             main parsing module -> parse_worker
│   ├── parse/
│   │   ├── csv.go          csv parse
│   │   ├── engine.go       parsing coordination
│   │   └── txt.go          txt parse
├── tests/                  everything below is for testing purposes only
│   ├── go/
│   └── python/
└── smtp_worker_test.go
```

---

## Configuration reference

```yaml
smtp:
  helo_hostname: "mail.yourdomain.com"  # REQUIRED — domain with A + PTR + SPF
  mail_from: "verify@yourdomain.com"    # REQUIRED — sender address
  connect_timeout_sec: 10
  command_timeout_sec: 15
  port: 25
  try_tls: true

concurrency:
  global_limit: 100       # total simultaneous SMTP connections
  per_domain_limit: 2     # max to any one domain (avoids hammering Gmail etc.)

dns:
  mx_cache_ttl: 3600          # seconds to cache MX records
  catch_all_cache_ttl: 3600   # seconds to cache catch-all probe results
  resolvers: []               # leave empty for system default

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
| `error` | Could not connect or worker crashed |

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