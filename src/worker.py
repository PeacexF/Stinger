# Interface to the compiled Go smtp_worker binary


from __future__ import annotations

import asyncio
import json
from pathlib import Path

WORKER_BINARY: Path = Path(__file__).parent / "smtp_worker"


async def call_worker(job: dict) -> dict:
    # Spawn the Go binary once per job.
    # Sends job as JSON in stdin, reads JSON from stdout.
    if not WORKER_BINARY.exists():
        return _err(job, f"smtp_worker binary not found at {WORKER_BINARY} — run: stinger build")

    try:
        proc = await asyncio.create_subprocess_exec(
            str(WORKER_BINARY),
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.DEVNULL,
        )
    except OSError as e:
        return _err(job, f"failed to start smtp_worker: {e}")

    payload = json.dumps(job).encode() + b"\n"
    hard_timeout = job.get("timeout_sec", 10) + 5

    try:
        stdout, _ = await asyncio.wait_for(proc.communicate(payload), timeout=hard_timeout)
    except asyncio.TimeoutError:
        proc.kill()
        return _err(job, "worker process timed out")
    except Exception as e:
        return _err(job, f"worker communication error: {e}")

    raw = stdout.decode("utf-8", errors="replace").strip()
    if not raw:
        return _err(job, "worker returned empty output")

    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return _err(job, f"worker returned invalid JSON: {raw[:120]}")


def build_job(email: str, mx: str, smtp_cfg: dict) -> dict:
    return {
        "email": email,
        "mx": mx,
        "helo": smtp_cfg["helo_hostname"],
        "mail_from": smtp_cfg["mail_from"],
        "timeout_sec": smtp_cfg.get("connect_timeout_sec", 10),
        "try_tls": smtp_cfg.get("try_tls", True),
        "port": smtp_cfg.get("port", 25),
    }

def _err(job: dict, msg: str) -> dict:
    return {
        "email": job.get("email", ""),
        "mx": job.get("mx", ""),
        "smtp_code": 0,
        "smtp_message": "",
        "error": msg,
        "tls_used": False,
        "duration_ms": 0,
    }