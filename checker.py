# Outdated, was separated into files, will remove this one from the repo in the future
# High-performance async SMTP email verifier.
# Orchestrates the Go smtp_worker binary for low-level socket work.


from __future__ import annotations

import asyncio
import json
import logging
import os
import re
import sys
import time
from collections import defaultdict
from dataclasses import dataclass, field, asdict
from datetime import datetime, timezone
from enum import Enum
from pathlib import Path
from typing import Optional

import dns.asyncresolver
import dns.exception
import yaml

WORKER_BINARY = Path(__file__).parent / "smtp_worker"
CATCH_ALL_PROBE = "stinger-catchall-probe-xqz@"

# SMTP codes that confirm a valid mailbox
VALID_CODES = {250, 251}

# Codes that mean "retry later"
RETRY_CODES = {421, 450, 451}

# Codes that definitively reject
INVALID_CODES = {550, 551, 552, 553, 554}


class Status(str, Enum):
    VALID = "valid"
    INVALID = "invalid"
    CATCH_ALL = "catch_all"
    UNKNOWN = "unknown"
    ERROR = "error"


@dataclass
class EmailResult:
    email: str
    status: Status
    smtp_code: Optional[int] = None
    smtp_message: Optional[str] = None
    mx_used: Optional[str] = None
    tls_used: bool = False
    is_catch_all_domain: bool = False
    attempts: int = 0
    duration_ms: int = 0
    reason: Optional[str] = None
    timestamp: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat())

    def to_jsonl(self) -> str:
        d = asdict(self)
        d["status"] = self.status.value
        return json.dumps(d)


# Config 
def load_config(path: str = "config.yaml") -> dict:
    with open(path) as f:
        cfg = yaml.safe_load(f)

    helo = cfg["smtp"].get("helo_hostname", "").strip()
    mail_from = cfg["smtp"].get("mail_from", "").strip()
    if not helo or not mail_from:
        print(
            "\n[SMTP-Stinger] ERROR: 'helo_hostname' and 'mail_from' must be set in config.yaml.\n"
            "  Use a real domain you control with proper A, PTR, and SPF records.\n"
            "  Refusing to start with blank/default values.\n"
        )
        sys.exit(1)

    return cfg


# DNS helpers 
class DNSCache:
    def __init__(self, cfg: dict):
        self.mx_cache: dict[str, list[str]] = {}
        self.mx_ttl: dict[str, float] = {}
        self.catch_all_cache: dict[str, bool] = {}
        self.catch_all_ttl: dict[str, float] = {}
        self.mx_ttl_sec = cfg["dns"]["mx_cache_ttl"]
        self.ca_ttl_sec = cfg["dns"]["catch_all_cache_ttl"]
        resolvers = cfg["dns"].get("resolvers") or []
        self.resolver = dns.asyncresolver.Resolver()
        if resolvers:
            self.resolver.nameservers = resolvers

    def _expired(self, ttl_map: dict, key: str, ttl_sec: int) -> bool:
        return key not in ttl_map or (time.monotonic() - ttl_map[key]) > ttl_sec

    async def get_mx(self, domain: str) -> list[str]:
        if not self._expired(self.mx_ttl, domain, self.mx_ttl_sec):
            return self.mx_cache.get(domain, [])
        try:
            answers = await self.resolver.resolve(domain, "MX")
            records = sorted(answers, key=lambda r: r.preference)
            hosts = [str(r.exchange).rstrip(".") for r in records]
            self.mx_cache[domain] = hosts
            self.mx_ttl[domain] = time.monotonic()
            return hosts
        except (dns.exception.NXDOMAIN, dns.exception.NoAnswer, dns.resolver.NoNameservers):
            self.mx_cache[domain] = []
            self.mx_ttl[domain] = time.monotonic()
            return []
        except dns.exception.DNSException:
            # Transient — don't cache, let caller retry
            raise

    async def get_catch_all(self, domain: str) -> Optional[bool]:
        if not self._expired(self.catch_all_ttl, domain, self.ca_ttl_sec):
            return self.catch_all_cache.get(domain)
        return None

    def set_catch_all(self, domain: str, value: bool):
        self.catch_all_cache[domain] = value
        self.catch_all_ttl[domain] = time.monotonic()


# Go worker
async def call_worker(job: dict) -> dict:
    proc = await asyncio.create_subprocess_exec(
        str(WORKER_BINARY),
        stdin=asyncio.subprocess.PIPE,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.DEVNULL,
    )
    payload = json.dumps(job).encode() + b"\n"
    try:
        stdout, _ = await asyncio.wait_for(
            proc.communicate(payload),
            timeout=job.get("timeout_sec", 10) + 5,
        )
    except asyncio.TimeoutError:
        proc.kill()
        return {"email": job["email"], "mx": job["mx"], "smtp_code": 0,
                "smtp_message": "", "error": "worker timeout", "tls_used": False, "duration_ms": 0}

    try:
        return json.loads(stdout.decode().strip())
    except json.JSONDecodeError:
        return {"email": job["email"], "mx": job["mx"], "smtp_code": 0,
                "smtp_message": "", "error": "worker bad JSON output", "tls_used": False, "duration_ms": 0}


# Retry logic
def classify_worker_result(res: dict) -> tuple[Status, str]:
    code = res.get("smtp_code", 0)
    error = res.get("error", "")
    msg = res.get("smtp_message", "")

    if error and not code:
        # Network/socket error — treat as retryable unknown
        return Status.UNKNOWN, f"network error: {error}"

    if code in VALID_CODES:
        return Status.VALID, msg

    if code in INVALID_CODES:
        return Status.INVALID, msg

    if code in RETRY_CODES:
        return Status.UNKNOWN, f"temp failure code {code}: {msg}"

    if code == 452:
        return Status.UNKNOWN, f"mailbox full or temp: {msg}"

    if code == 501:
        return Status.INVALID, f"syntax error: {msg}"

    if code == 503:
        return Status.UNKNOWN, f"sequence error: {msg}"

    if 400 <= code < 500:
        return Status.UNKNOWN, f"4xx temp: {code} {msg}"

    if 500 <= code < 600:
        return Status.INVALID, f"5xx perm: {code} {msg}"

    return Status.UNKNOWN, f"unclassified: code={code} err={error}"


# Core verifier
class Verifier:
    def __init__(self, cfg: dict):
        self.cfg = cfg
        self.dns = DNSCache(cfg)
        self.global_sem = asyncio.Semaphore(cfg["concurrency"]["global_limit"])
        self.domain_sems: dict[str, asyncio.Semaphore] = defaultdict(
            lambda: asyncio.Semaphore(cfg["concurrency"]["per_domain_limit"])
        )
        self.max_attempts = cfg["retry"]["max_attempts"]
        self.backoff_base = cfg["retry"]["backoff_base_sec"]
        self.smtp_cfg = cfg["smtp"]
        # Progress tracking
        self.total = 0
        self.done = 0
        self._lock = asyncio.Lock()

    def _worker_job(self, email: str, mx: str) -> dict:
        return {
            "email": email,
            "mx": mx,
            "helo": self.smtp_cfg["helo_hostname"],
            "mail_from": self.smtp_cfg["mail_from"],
            "timeout_sec": self.smtp_cfg["connect_timeout_sec"],
            "try_tls": self.smtp_cfg.get("try_tls", True),
            "port": self.smtp_cfg.get("port", 25),
        }

    async def _probe_once(self, email: str, mx: str) -> dict:
        async with self.global_sem:
            domain = email.split("@")[1]
            async with self.domain_sems[domain]:
                return await call_worker(self._worker_job(email, mx))

    async def _detect_catch_all(self, domain: str, mxs: list[str]) -> bool:
        cached = await self.dns.get_catch_all(domain)
        if cached is not None:
            return cached

        probe_addr = f"{CATCH_ALL_PROBE}{domain}"
        for mx in mxs[:2]:  # only try first 2 MX records
            res = await self._probe_once(probe_addr, mx)
            code = res.get("smtp_code", 0)
            if code in VALID_CODES:
                self.dns.set_catch_all(domain, True)
                return True
            if code in INVALID_CODES or (code == 0 and not res.get("error")):
                self.dns.set_catch_all(domain, False)
                return False
        # Inconclusive — assume not catch-all
        self.dns.set_catch_all(domain, False)
        return False

    async def verify(self, email: str) -> EmailResult:
        email = email.strip().lower()
        domain = email.split("@")[1] if "@" in email else ""

        if not domain or not _valid_email(email):
            return EmailResult(
                email=email, status=Status.INVALID, reason="malformed email address"
            )

        # DNS: get MX records
        try:
            mxs = await self.dns.get_mx(domain)
        except dns.exception.DNSException:
            mxs = []

        if not mxs:
            return EmailResult(
                email=email, status=Status.INVALID,
                reason="no MX records found (NXDOMAIN or no answer)"
            )

        # Check catch-all (once per domain, cached)
        is_catch_all = await self._detect_catch_all(domain, mxs)

        # Probe with retry + MX fallback
        attempts = 0
        last_res: dict = {}
        last_status = Status.UNKNOWN
        last_reason = ""

        for attempt in range(self.max_attempts):
            for mx in mxs:
                attempts += 1
                res = await self._probe_once(email, mx)
                status, reason = classify_worker_result(res)

                if status == Status.VALID:
                    final = Status.CATCH_ALL if is_catch_all else Status.VALID
                    return EmailResult(
                        email=email,
                        status=final,
                        smtp_code=res.get("smtp_code"),
                        smtp_message=res.get("smtp_message"),
                        mx_used=mx,
                        tls_used=res.get("tls_used", False),
                        is_catch_all_domain=is_catch_all,
                        attempts=attempts,
                        duration_ms=res.get("duration_ms", 0),
                        reason=reason,
                    )

                if status == Status.INVALID:
                    return EmailResult(
                        email=email, status=Status.INVALID,
                        smtp_code=res.get("smtp_code"),
                        smtp_message=res.get("smtp_message"),
                        mx_used=mx, tls_used=res.get("tls_used", False),
                        is_catch_all_domain=is_catch_all,
                        attempts=attempts,
                        duration_ms=res.get("duration_ms", 0),
                        reason=reason,
                    )

                # UNKNOWN / retryable — try next MX first, then backoff
                last_res = res
                last_status = status
                last_reason = reason

            # All MX tried, wait before next attempt round
            if attempt < self.max_attempts - 1:
                await asyncio.sleep(self.backoff_base * (2 ** attempt))

        return EmailResult(
            email=email,
            status=last_status,
            smtp_code=last_res.get("smtp_code"),
            smtp_message=last_res.get("smtp_message"),
            mx_used=mxs[0] if mxs else None,
            tls_used=last_res.get("tls_used", False),
            is_catch_all_domain=is_catch_all,
            attempts=attempts,
            duration_ms=last_res.get("duration_ms", 0),
            reason=last_reason,
        )

    async def _update_progress(self):
        async with self._lock:
            self.done += 1
            if self.cfg["logging"]["show_progress"]:
                pct = self.done / self.total * 100 if self.total else 0
                print(f"\r  {self.done}/{self.total} ({pct:.1f}%)", end="", flush=True)


# Email validation
_EMAIL_RE = re.compile(r"^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$")

def _valid_email(email: str) -> bool:
    return bool(_EMAIL_RE.match(email))


# Output helpers
def setup_output(cfg: dict) -> tuple[Path, Path]:
    out_dir = Path(cfg["output"]["output_dir"])
    out_dir.mkdir(parents=True, exist_ok=True)
    valid_path = out_dir / cfg["output"]["valid_txt"]
    jsonl_path = out_dir / cfg["output"]["full_jsonl"]
    return valid_path, jsonl_path


# Main runner
async def run(cfg: dict):
    emails_file = Path(cfg["input"]["emails_file"])
    if not emails_file.exists():
        print(f"[SMTP-Stinger] ERROR: emails file not found: {emails_file}")
        sys.exit(1)

    if not WORKER_BINARY.exists():
        print(
            f"[SMTP-Stinger] ERROR: Go worker binary not found at {WORKER_BINARY}\n"
            f"  Build it with:  go build -o smtp_worker smtp_worker.go"
        )
        sys.exit(1)

    emails = [
        line.strip() for line in emails_file.read_text().splitlines()
        if line.strip() and not line.startswith("#")
    ]
    emails = list(dict.fromkeys(emails))  # deduplicate, preserve order

    if not emails:
        print("[SMTP-Stinger] No emails to process.")
        sys.exit(0)

    valid_path, jsonl_path = setup_output(cfg)

    verifier = Verifier(cfg)
    verifier.total = len(emails)

    log_level = getattr(logging, cfg["logging"].get("level", "INFO").upper(), logging.INFO)
    logging.basicConfig(level=log_level, format="%(asctime)s [%(levelname)s] %(message)s")
    log = logging.getLogger("smtp_stinger")

    print(f"\n{'═' * 56}")
    print(f"  SMTP-Stinger  —  {len(emails)} emails to verify")
    print(f"  helo:      {cfg['smtp']['helo_hostname']}")
    print(f"  mail_from: {cfg['smtp']['mail_from']}")
    print(f"  workers:   {cfg['concurrency']['global_limit']} global / "
          f"{cfg['concurrency']['per_domain_limit']} per domain")
    print(f"{'═' * 56}\n")

    start = time.monotonic()
    counters: dict[str, int] = defaultdict(int)

    # Open output files and stream results
    with open(valid_path, "w") as vf, open(jsonl_path, "w") as jf:
        async def process(email: str):
            result = await verifier.verify(email)
            await verifier._update_progress()

            # Write JSONL for every email
            jf.write(result.to_jsonl() + "\n")
            jf.flush()

            # Write to valid.txt if status is VALID or CATCH_ALL (both had 250/251)
            if result.status in (Status.VALID, Status.CATCH_ALL):
                vf.write(result.email + "\n")
                vf.flush()

            counters[result.status.value] += 1
            log.debug("[%s] %s — %s", result.status.value, email, result.reason or "")

        tasks = [process(e) for e in emails]
        await asyncio.gather(*tasks)

    elapsed = time.monotonic() - start
    rate = len(emails) / elapsed if elapsed > 0 else 0

    print(f"\n\n{'═' * 56}")
    print(f"  Done in {elapsed:.1f}s  ({rate:.1f} emails/sec)")
    print(f"  valid      : {counters.get('valid', 0)}")
    print(f"  catch_all  : {counters.get('catch_all', 0)}")
    print(f"  invalid    : {counters.get('invalid', 0)}")
    print(f"  unknown    : {counters.get('unknown', 0)}")
    print(f"  error      : {counters.get('error', 0)}")
    print(f"\n  → {valid_path}")
    print(f"  → {jsonl_path}")
    print(f"{'═' * 56}\n")


def main():
    import argparse
    parser = argparse.ArgumentParser(description="SMTP-Stinger — high-performance SMTP verifier")
    parser.add_argument("--config", default="config.yaml", help="Path to config.yaml")
    args = parser.parse_args()

    cfg = load_config(args.config)
    asyncio.run(run(cfg))


if __name__ == "__main__":
    main()