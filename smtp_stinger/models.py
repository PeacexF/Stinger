# Shared dataclasses and models

from __future__ import annotations

import json
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from enum import Enum
from typing import Optional


class Status(str, Enum):
    VALID     = "valid"
    INVALID   = "invalid"
    CATCH_ALL = "catch_all"
    UNKNOWN   = "unknown"
    ERROR     = "error"


class SubStatus(str, Enum):
    # Precise classification

    # valid
    CONFIRMED        = "confirmed"         # clean 250/251

    # catch-all
    CATCH_ALL        = "catch_all"         # domain accepts any

    # invalid 
    MAILBOX_NOT_FOUND = "mailbox_not_found" # 550/551 — user does not exist
    MAILBOX_FULL      = "mailbox_full"      # 552 — over quota
    DOMAIN_REJECTED   = "domain_rejected"   # 553 — bad sender/rcpt at domain level
    SPAM_BLOCK        = "spam_block"        # 554 — policy/reputation rejection
    SYNTAX_ERROR      = "syntax_error"      # 501 — malformed address
    NO_MX             = "no_mx"             # NXDOMAIN / no MX records
    MALFORMED         = "malformed"         # empty label, bad format

    # unknown
    GREYLISTED        = "greylisted"        # 451 — try again later
    RATE_LIMITED      = "rate_limited"      # 421 — too many connections
    MAILBOX_TEMP      = "mailbox_temp"      # 450 — mailbox temporarily unavailable
    CONNECT_FAILED    = "connect_failed"    # TCP timeout / connection refused
    DNS_TIMEOUT       = "dns_timeout"       # DNS query timed out
    DNS_ERROR         = "dns_error"         # other DNS failure
    WORKER_ERROR      = "worker_error"      # Go worker failed / crashed
    TEMP_FAILURE      = "temp_failure"      # other 4xx not specifically classified


VALID_CODES   = {250, 251}
RETRY_CODES   = {421, 450, 451}
INVALID_CODES = {550, 551, 552, 553, 554}


@dataclass
class EmailResult:
    email: str
    status: Status
    sub_status: Optional[SubStatus] = None
    smtp_code: Optional[int] = None
    smtp_message: Optional[str] = None
    mx_used: Optional[str] = None
    tls_used: bool = False
    is_catch_all_domain: bool = False
    attempts: int = 0
    duration_ms: int = 0
    reason: Optional[str] = None
    timestamp: str = field(
        default_factory=lambda: datetime.now(timezone.utc).isoformat()
    )

    def to_jsonl(self) -> str:
        d = asdict(self)
        d["status"] = self.status.value
        d["sub_status"] = self.sub_status.value if self.sub_status else None
        return json.dumps(d)


def classify_worker_result(res: dict) -> tuple[Status, SubStatus, str]:
    code  = res.get("smtp_code", 0)
    error = res.get("error", "")
    msg   = res.get("smtp_message", "") or ""

    # Network / worker errors
    if error and not code:
        if any(x in error.lower() for x in ("timeout", "timed out", "deadline")):
            return Status.UNKNOWN, SubStatus.CONNECT_FAILED, f"connection timed out: {error}"
        if any(x in error.lower() for x in ("refused", "connect failed", "connection refused")):
            return Status.UNKNOWN, SubStatus.CONNECT_FAILED, f"connection refused: {error}"
        if "worker" in error.lower():
            return Status.UNKNOWN, SubStatus.WORKER_ERROR, f"worker error: {error}"
        return Status.UNKNOWN, SubStatus.CONNECT_FAILED, f"network error: {error}"

    # valid 
    if code in VALID_CODES:
        return Status.VALID, SubStatus.CONFIRMED, msg

    # permanent
    if code in (550, 551):
        return Status.INVALID, SubStatus.MAILBOX_NOT_FOUND, f"user not found: {msg}"
    if code == 552:
        return Status.INVALID, SubStatus.MAILBOX_FULL, f"mailbox full: {msg}"
    if code == 553:
        return Status.INVALID, SubStatus.DOMAIN_REJECTED, f"domain rejected: {msg}"
    if code == 554:
        # 554 can be either a spam/reputation block or a generic perm failure
        low = msg.lower()
        if any(x in low for x in ("spam", "policy", "blocked", "blacklist", "dnsbl", "reputation")):
            return Status.INVALID, SubStatus.SPAM_BLOCK, f"spam/policy block: {msg}"
        return Status.INVALID, SubStatus.DOMAIN_REJECTED, f"permanent rejection: {msg}"
    if code == 501:
        return Status.INVALID, SubStatus.SYNTAX_ERROR, f"syntax error: {msg}"
    if code in INVALID_CODES:
        return Status.INVALID, SubStatus.DOMAIN_REJECTED, f"permanent rejection {code}: {msg}"

    # retryable
    if code == 421:
        return Status.UNKNOWN, SubStatus.RATE_LIMITED, f"rate limited (421): {msg}"
    if code == 450:
        return Status.UNKNOWN, SubStatus.MAILBOX_TEMP, f"mailbox temp unavailable (450): {msg}"
    if code == 451:
        # 451 is the canonical greylisting response
        return Status.UNKNOWN, SubStatus.GREYLISTED, f"greylisted (451): {msg}"
    if code == 452:
        return Status.UNKNOWN, SubStatus.MAILBOX_FULL, f"mailbox full / temp (452): {msg}"
    if code == 503:
        return Status.UNKNOWN, SubStatus.TEMP_FAILURE, f"sequence error (503): {msg}"
    if 400 <= code < 500:
        return Status.UNKNOWN, SubStatus.TEMP_FAILURE, f"temp failure {code}: {msg}"

    # catch-all (set by verifier)
    if 500 <= code < 600:
        return Status.INVALID, SubStatus.DOMAIN_REJECTED, f"perm rejection {code}: {msg}"

    return Status.UNKNOWN, SubStatus.TEMP_FAILURE, f"unclassified: code={code} err={error}"