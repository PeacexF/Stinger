# Checkpoint read/write for graceful interruption and resume.
#
# line 1 — header:  {"type":"header","emails_file":"...","total":45797,"timestamp":"..."}
# next lines — one completed email per line:  {"type":"email","email":"alice@example.com"}

from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path

CHECKPOINT_FILENAME = "checkpoint.jsonl"


class Checkpoint:
    def __init__(self, output_dir: Path, emails_file: str, total: int):
        self.path = output_dir / CHECKPOINT_FILENAME
        self.emails_file = emails_file
        self.total = total
        self._completed: set[str] = set()

    def mark(self, email: str) -> None:
        self._completed.add(email)

    @property
    def completed(self) -> set[str]:
        return self._completed

    def save(self) -> None:
        header = {
            "type": "header",
            "emails_file": self.emails_file,
            "total": self.total,
            "completed": len(self._completed),
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }
        tmp = self.path.with_suffix(".tmp")
        with open(tmp, "w", encoding="utf-8") as f:
            f.write(json.dumps(header) + "\n")
            for email in sorted(self._completed):
                f.write(json.dumps({"type": "email", "email": email}) + "\n")
        tmp.replace(self.path)

    def delete(self) -> None:
        if self.path.exists():
            self.path.unlink()

    @staticmethod
    def load(path: Path) -> set[str]:
        if not path.exists():
            raise ValueError(f"Checkpoint file not found: {path}")

        completed: set[str] = set()
        try:
            with open(path, encoding="utf-8") as f:
                for i, line in enumerate(f):
                    line = line.strip()
                    if not line:
                        continue
                    rec = json.loads(line)
                    # Skip the header line, collect email lines
                    if rec.get("type") == "email":
                        email = rec.get("email", "").strip()
                        if email:
                            completed.add(email)
        except json.JSONDecodeError as e:
            raise ValueError(f"Checkpoint file is corrupt (line {i + 1}): {e}")

        return completed

    @staticmethod
    def find(output_dir: Path) -> Path | None:
        p = output_dir / CHECKPOINT_FILENAME
        return p if p.exists() else None