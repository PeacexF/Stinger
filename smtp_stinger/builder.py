# Compiles smtp_worker.go into a binary that lives alongside the package
# Called by `stinger build`


from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path

PACKAGE_DIR = Path(__file__).parent

BINARIES = {
    "smtp_worker": {
        "out":  PACKAGE_DIR / "smtp_worker",
        "src_candidates": [
            PACKAGE_DIR / "smtp_worker.go",
            PACKAGE_DIR.parent / "smtp_worker.go",
        ],
        "is_package": False,
    },
    "parse_worker": {
        "out":  PACKAGE_DIR / "parse_worker",
        "src_candidates": [
            PACKAGE_DIR / "parse",
            PACKAGE_DIR.parent / "parse",
        ],
        "is_package": True,
    },
}


def build(verbose: bool = True) -> bool:
    go = shutil.which("go")
    if not go:
        _err(
            "Go compiler not found.\n"
            "  Install Go from https://go.dev/dl/ then re-run:  stinger build"
        )
        return False

    all_ok = True
    for name, spec in BINARIES.items():
        ok = _build_one(go, name, spec, verbose)
        if not ok:
            all_ok = False

    return all_ok


def _build_one(go: str, name: str, spec: dict, verbose: bool) -> bool:
    out: Path = spec["out"]
    out.parent.mkdir(parents=True, exist_ok=True)

    src = None
    for candidate in spec["src_candidates"]:
        if candidate.exists():
            src = candidate
            break

    if src is None:
        _err(f"Source for {name} not found. Tried: {spec['src_candidates']}")
        return False

    if verbose:
        label = str(src.relative_to(src.parent.parent)) if src.parent != src else src.name
        print(f"[stinger build] go build  {label} → {out.relative_to(out.parent.parent)}")

    if spec["is_package"]:
        cmd = [go, "build", "-o", str(out), str(src)]
    else:
        cmd = [go, "build", "-o", str(out), str(src)]

    result = subprocess.run(cmd, capture_output=True, text=True)

    if result.returncode != 0:
        _err(f"{name} compilation failed:\n{result.stderr}")
        return False

    size_kb = out.stat().st_size // 1024
    if verbose:
        print(f"[stinger build] ✓ {name}  ({size_kb} KB)")

    return True


def _err(msg: str) -> None:
    print(f"[stinger build] ERROR: {msg}", file=sys.stderr)