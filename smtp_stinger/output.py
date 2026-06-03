# Output writers and stats summariser for SMTP-Stinger


from __future__ import annotations

import json
from collections import defaultdict
from pathlib import Path
from typing import IO

from .models import EmailResult, Status


class ResultWriter:
    # Streams into valid.txt and results.jsonl

    def __init__(self, cfg: dict, append: bool = False, checkpoint=None):
        out_dir = Path(cfg["output"]["output_dir"])
        out_dir.mkdir(parents=True, exist_ok=True)
        self.valid_path = out_dir / cfg["output"]["valid_txt"]
        self.jsonl_path = out_dir / cfg["output"]["full_jsonl"]
        self._mode = "a" if append else "w"
        self._vf: IO | None = None
        self._jf: IO | None = None
        self.counters: dict[str, int] = defaultdict(int)
        self._checkpoint = checkpoint

    def open(self) -> None:
        self._vf = open(self.valid_path, self._mode)
        self._jf = open(self.jsonl_path, self._mode)

    def write(self, result: EmailResult) -> None:
        assert self._vf and self._jf, "call open() first"

        self._jf.write(result.to_jsonl() + "\n")
        self._jf.flush()

        if result.status in (Status.VALID, Status.CATCH_ALL):
            self._vf.write(result.email + "\n")
            self._vf.flush()

        self.counters[result.status.value] += 1

        if self._checkpoint:
            self._checkpoint.mark(result.email)

    def close(self) -> None:
        if self._vf:
            self._vf.close()
        if self._jf:
            self._jf.close()

    def __enter__(self):
        self.open()
        return self

    def __exit__(self, *_):
        self.close()


def summarise_jsonl(jsonl_path: Path) -> dict:
    # Read results.jsonl and return stats
    counts: dict[str, int] = defaultdict(int)
    total_ms = 0
    total = 0
    catch_all_domains: set[str] = set()
    errors: list[str] = []

    with open(jsonl_path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                rec = json.loads(line)
            except json.JSONDecodeError:
                continue
            total += 1
            status = rec.get("status", "unknown")
            counts[status] += 1
            total_ms += rec.get("duration_ms", 0)
            if rec.get("is_catch_all_domain"):
                domain = rec["email"].split("@")[1] if "@" in rec["email"] else ""
                if domain:
                    catch_all_domains.add(domain)
            if status == "error":
                errors.append(f"  {rec['email']}: {rec.get('reason', '?')}")

    return {
        "total": total,
        "counts": dict(counts),
        "avg_duration_ms": round(total_ms / total, 1) if total else 0,
        "catch_all_domains": sorted(catch_all_domains),
        "sample_errors": errors[:10],
    }