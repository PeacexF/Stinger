# comments a bit later

from __future__ import annotations

import json
import subprocess
from dataclasses import dataclass
from pathlib import Path

PARSE_BINARY = Path(__file__).parent / "parse_worker"
SUPPORTED_EXTENSIONS = {".txt", ".csv"}


@dataclass
class ParseResult:
    emails: list[str]
    total_raw: int
    duplicates_removed: int
    unique: int
    files_parsed: list[str]
    files_skipped: list[str]
    per_file_unique: dict[str, int]
    error: str = ""


def collect_files(sources: list[str]) -> tuple[list[Path], list[Path]]:
    accepted: list[Path] = []
    skipped: list[Path] = []
    seen: set[Path] = set()

    def _add(p: Path) -> None:
        resolved = p.resolve()
        if resolved in seen:
            return
        seen.add(resolved)
        if p.suffix.lower() in SUPPORTED_EXTENSIONS:
            accepted.append(p)
        else:
            skipped.append(p)

    for raw in sources:
        p = Path(raw)

        if p.is_dir():
            for ext in SUPPORTED_EXTENSIONS:
                for found in sorted(p.rglob(f"*{ext}")):
                    _add(found)

        elif "*" in raw or "?" in raw:
            parent = p.parent if str(p.parent) != "." else Path(".")
            matches = sorted(parent.glob(p.name))
            if not matches:
                skipped.append(p)
            for match in matches:
                if match.is_file():
                    _add(match)

        elif p.is_file():
            _add(p)

        else:
            skipped.append(p)

    return accepted, skipped


def run_parse(
    sources: list[str],
    output_path: Path,
    workers: int = 4,
    append_to: Path | None = None,
) -> ParseResult:

    if not PARSE_BINARY.exists():
        return ParseResult(
            emails=[], total_raw=0, duplicates_removed=0, unique=0,
            files_parsed=[], files_skipped=[],
            per_file_unique={},
            error=f"parse_worker binary not found at {PARSE_BINARY} — run: stinger build",
        )

    accepted, skipped = collect_files(sources)

    if not accepted:
        return ParseResult(
            emails=[], total_raw=0, duplicates_removed=0, unique=0,
            files_parsed=[],
            files_skipped=[str(p) for p in skipped],
            per_file_unique={},
            error="no supported files found",
        )

    paths = [str(p.resolve()) for p in accepted]

    existing_file: Path | None = None
    if append_to and append_to.exists():
        existing_file = append_to
        paths = [str(append_to.resolve())] + paths

    job = {
        "paths": paths,
        "output_path": str(output_path.resolve()),
        "workers": workers,
    }

    try:
        proc = subprocess.run(
            [str(PARSE_BINARY)],
            input=json.dumps(job),
            capture_output=True,
            text=True,
            timeout=300,
        )
    except subprocess.TimeoutExpired:
        return _err_result(skipped, "parse_worker timed out after 300s")
    except OSError as e:
        return _err_result(skipped, f"failed to start parse_worker: {e}")

    raw = proc.stdout.strip()
    if not raw:
        stderr = proc.stderr.strip()
        return _err_result(skipped, f"parse_worker returned no output. stderr: {stderr}")

    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        return _err_result(skipped, f"parse_worker returned invalid JSON: {raw[:120]}")

    if data.get("error"):
        return _err_result(skipped, data["error"])

    emails: list[str] = []
    if output_path.exists():
        emails = [
            line.strip() for line in output_path.read_text(encoding="utf-8").splitlines()
            if line.strip()
        ]

    per_file = data.get("per_file_unique") or {}
    if existing_file:
        per_file.pop(str(existing_file.resolve()), None)

    all_skipped = list(data.get("files_skipped") or []) + [str(p) for p in skipped]
    files_parsed = [
        f for f in (data.get("files_parsed") or [])
        if existing_file is None or f != str(existing_file.resolve())
    ]

    return ParseResult(
        emails=emails,
        total_raw=int(data.get("total_raw", 0)),
        duplicates_removed=int(data.get("duplicates_removed", 0)),
        unique=int(data.get("unique", 0)),
        files_parsed=files_parsed,
        files_skipped=all_skipped,
        per_file_unique={k: int(v) for k, v in per_file.items()},
        error="",
    )


def _err_result(skipped: list[Path], msg: str) -> ParseResult:
    return ParseResult(
        emails=[], total_raw=0, duplicates_removed=0, unique=0,
        files_parsed=[],
        files_skipped=[str(p) for p in skipped],
        per_file_unique={},
        error=msg,
    )