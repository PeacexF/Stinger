# Extracts and deduplicates email addresses from .txt and .csv files.
# handles single files, multiple files, and directory glob patterns.
# Called by the `stinger parse` cmd, values that can be passed are: path/to/file.[csv][txt] | ./dir | path/to/dir/*
# Maybe the parsers will be remade in Go in the future (because parsing a lot of big .csv files will be slow with the current logic)

from __future__ import annotations

import csv 
import io
import re
from dataclasses import dataclass, field
from pathlib import Path

# RFC-5321 email pattern — intentionally permissive for extraction
_EMAIL_RE = re.compile(r"[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}")

SUPPORTED_EXTENSIONS = {".txt", ".csv"}


@dataclass
class ParseResult:
    emails: list[str]                        # deduplicated, lowercased, ordered by first seen
    total_raw: int                           # total email-like strings found before dedup
    duplicates_removed: int                  # how many were dropped as dupes
    files_parsed: list[Path]                 # every file that was read
    files_skipped: list[Path]                # files that were ignored (unsupported ext, unreadable)
    per_file: dict[str, int] = field(default_factory=dict)  # filename -> unique emails found


def collect_files(paths: list[str]) -> tuple[list[Path], list[Path]]:
    # Expand a list of paths into .txt/.csv files.
    # Each entry can be:
    # -> a single file              e.g. emails.txt
    # -> a directory                e.g. ./data   (scans recursively for .txt/.csv)
    # -> a glob pattern             e.g. data/*   or data/**/*.csv

    # Returns (accepted, skipped).
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

    for raw in paths:
        p = Path(raw)

        if p.is_dir():
            for ext in SUPPORTED_EXTENSIONS:
                for found in sorted(p.rglob(f"*{ext}")):
                    _add(found)

        elif "*" in raw or "?" in raw:
            # Glob pattern
            parent = p.parent if p.parent != Path(".") else Path(".")
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


def extract_from_txt(text: str) -> list[str]:
    return _EMAIL_RE.findall(text)


def extract_from_csv(text: str) -> list[str]:
    # Try to parse as CSV first, scanning every cell for emails.
    # Falls back to plain regex extraction if CSV parsing fails.
    found: list[str] = []
    try:
        reader = csv.reader(io.StringIO(text))
        for row in reader:
            for cell in row:
                cell = cell.strip()
                if _EMAIL_RE.fullmatch(cell):
                    found.append(cell)
                else:
                    found.extend(_EMAIL_RE.findall(cell))
    except csv.Error:
        # Malformed CSV — fall back to regex on the whole text
        found = _EMAIL_RE.findall(text)
    return found


def parse_file(path: Path) -> tuple[list[str], bool]:
    try:
        text = path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return [], False

    ext = path.suffix.lower()
    if ext == ".csv":
        return extract_from_csv(text), True
    else:
        return extract_from_txt(text), True


def parse_paths(paths: list[str]) -> ParseResult:
    # Main entry point. Accepts a list of path strings (files, dirs, globs),
    # extracts all emails, deduplicates, and returns a ParseResult.
    accepted, initially_skipped = collect_files(paths)
    skipped = list(initially_skipped)

    seen: dict[str, None] = {}   # ordered set via dict
    total_raw = 0
    files_parsed: list[Path] = []
    per_file: dict[str, int] = {}

    for path in accepted:
        raw_emails, ok = parse_file(path)
        if not ok:
            skipped.append(path)
            continue

        files_parsed.append(path)
        before = len(seen)

        for email in raw_emails:
            total_raw += 1
            normalised = email.strip().lower()
            seen[normalised] = None

        new_unique = len(seen) - before
        per_file[str(path)] = new_unique

    emails = list(seen.keys())
    duplicates_removed = total_raw - len(emails)

    return ParseResult(
        emails=emails,
        total_raw=total_raw,
        duplicates_removed=duplicates_removed,
        files_parsed=files_parsed,
        files_skipped=skipped,
        per_file=per_file,
    )