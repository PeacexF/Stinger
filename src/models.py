# Shared dataclasses and models

from __future__ import annotations

import json
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from enum import Enum
from typing import Optional


class Status(str, Enum):
    VALID = "valid"
    INVALID = "invalid"
    CATCH_ALL = "catch_all"
    UNKNOWN = "unknown"
    ERROR = "error"


# SMTP response codes
VALID_CODES = {250, 251}
RETRY_CODES = {421, 450, 451}
INVALID_CODES = {550, 551, 552, 553, 554}


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
    timestamp: str = field(
        default_factory=lambda: datetime.now(timezone.utc).isoformat()
    )

    def to_jsonl(self) -> str:
        d = asdict(self)
        d["status"] = self.status.value
        return json.dumps(d)


def classify_worker_result(res: dict) -> tuple[Status, str]:
    code = res.get("smtp_code", 0)
    error = res.get("error", "")
    msg = res.get("smtp_message", "")

    if error and not code:
        return Status.UNKNOWN, f"network error: {error}"
    if code in VALID_CODES:
        return Status.VALID, msg
    if code in INVALID_CODES:
        return Status.INVALID, msg
    if code in RETRY_CODES:
        return Status.UNKNOWN, f"temp failure {code}: {msg}"
    if code == 452:
        return Status.UNKNOWN, f"mailbox full / temp: {msg}"
    if code == 501:
        return Status.INVALID, f"syntax error: {msg}"
    if code == 503:
        return Status.UNKNOWN, f"sequence error: {msg}"
    if 400 <= code < 500:
        return Status.UNKNOWN, f"4xx temp: {code} {msg}"
    if 500 <= code < 600:
        return Status.INVALID, f"5xx perm: {code} {msg}"
    return Status.UNKNOWN, f"unclassified: code={code} err={error}"