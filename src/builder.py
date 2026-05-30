# Compiles smtp_worker.go into a binary that lives alongside the package
# Called by `stinger build`


from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path


def _go_source() -> Path | None:
    candidates = [
        Path(__file__).parent / "smtp_worker.go",        # bundled inside package
        Path(__file__).parent.parent / "smtp_worker.go", # project root (dev)
    ]
    for p in candidates:
        if p.exists():
            return p
    return None


def output_path() -> Path:
    return Path(__file__).parent / "smtp_worker"


def build(verbose: bool = True) -> bool:
    # Compile smtp_worker.go → src/smtp_worker binary.

    go = shutil.which("go")
    if not go:
        _err(
            "Go compiler not found.\n"
            "  Install Go from https://go.dev/dl/ then re-run:  stinger build"
        )
        return False

    src = _go_source()
    if src is None:
        _err(
            "smtp_worker.go not found.\n"
            "  Make sure you have the full smtp-stinger source."
        )
        return False

    out = output_path()
    out.parent.mkdir(parents=True, exist_ok=True)

    if verbose:
        print(f"[stinger build] go build  {src.name} → {out}")

    result = subprocess.run(
        [go, "build", "-o", str(out), str(src)],
        capture_output=True,
        text=True,
    )

    if result.returncode != 0:
        _err(f"Compilation failed:\n{result.stderr}")
        return False

    size_kb = out.stat().st_size // 1024
    if verbose:
        print(f"[stinger build] ✓ OK — {out}  ({size_kb} KB)")

    return True


def _err(msg: str) -> None:
    print(f"[stinger build] ERROR: {msg}", file=sys.stderr)