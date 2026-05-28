# SMTP-Stinger

High-performance SMTP email verifier. Python async orchestration + Go low-level socket worker.

---

## Architecture

```
smtp_checker/
├── checker.py        ← Python orchestrator (asyncio, DNS cache, retry logic, output)
├── smtp_worker.go    ← Go binary (raw SMTP, STARTTLS, fast sockets)
├── config.yaml       ← All configuration lives here
├── emails.txt        ← Your input list (one email per line)
└── results/
    ├── valid_emails.txt   ← Emails that returned 250/251
    └── results.jsonl      ← Full data for every email checked
```

---

## Status
I want to make it a pip installable cli tool, so it's actively developed and tested rn  
Will separate the project's files for readlability and add full CLI functionlity in the nearby future  
Tried to make the code well commented for anyone interested

---

## Prerequisites

- Python 3.10+
- Go 1.20+
- Python packages: `dnspython`, `pyyaml`

```bash
pip install dnspython pyyaml
```

---

## Build the Go worker

```bash
go build -o smtp_worker smtp_worker.go
```

---

## Configuration (config.yaml)

**Before running**, you MUST set two fields in `config.yaml`:

```yaml
smtp:
  helo_hostname: "mail.yourdomain.com"   # ← your real domain
  mail_from: "stinger@yourdomain.com"     # ← your real address
```

### Why this matters

SMTP-Stinger refuses to start with blank values. Here's why:

| What to set | Why |
|---|---|
| **A record** for your domain | Server must resolve your hostname |
| **PTR / rDNS** matching your IP | Many servers check reverse DNS |
| **SPF record** (`v=spf1 ip4:<YOUR_IP> ~all`) | Reduces rejection rate significantly |

Using a throwaway domain dedicated to probing is common practice. It doesn't need to receive email, just pass basic DNS checks.

---

## Running

```bash
python checker.py
# or with a custom config:
python checker.py --config /path/to/config.yaml
```

---

## Input format

`emails.txt` — one email per line, blank lines and `#` comments ignored:

```
alice@example.com
bob@company.org
# this line is ignored
carol@domain.net
```

Duplicates are automatically removed.

---

## Output

### `results/valid_emails.txt`

Plain list of emails that received a `250` or `251` SMTP response. Includes catch-all domains where SMTP confirmed the address (see catch-all handling below).

```
alice@example.com
carol@domain.net
```

### `results/results.jsonl`

One JSON object per line for every email checked:

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
  "duration_ms": 342,
  "reason": "2.1.5 OK",
  "timestamp": "2025-01-15T10:23:45.123456+00:00"
}
```

#### Status values

| Status | Meaning |
|---|---|
| `valid` | 250/251 response, domain is not catch-all |
| `catch_all` | 250/251 response, but domain accepts anything — address included in `valid_emails.txt` |
| `invalid` | 550/551/55x permanent rejection |
| `unknown` | Temp failure (421/450/451), greylisting, or network issue after all retries |
| `error` | Could not connect or worker failed |

---

## Catch-all handling

Before checking any address on a domain, SMTP-Stinger probes a random impossible address (e.g. `stinger-catchall-probe-xqz@domain.com`).

- If that probe returns **250/251** → domain is catch-all
- Result is cached for the domain (all further addresses skip this probe)
- Catch-all emails still appear in `valid_emails.txt` (they got 250/251) with `status: catch_all` in the JSONL

---

## Retry logic

| SMTP code | Action |
|---|---|
| 250, 251 | Valid — done |
| 550–554 | Invalid — done |
| 421, 450, 451 | Retry (up to `max_attempts`) |
| Network error | Retry next MX, then backoff |
| NXDOMAIN | Invalid — no retry |

MX fallback order: tries each MX record in priority order before backing off.

---

## Concurrency

No artificial sleeps. Instead:

- `global_limit` — max simultaneous open SMTP connections
- `per_domain_limit` — max connections to any single domain (avoids hammering Gmail/Microsoft)

Tune both in `config.yaml` based on your IP reputation and target volume.

---

## Performance expectations

| Volume | Estimated time |
|---|---|
| 1,000 emails | ~1–3 min |
| 10,000 emails | ~10–30 min |

Actual speed depends heavily on MX server response times and how aggressively servers throttle you.