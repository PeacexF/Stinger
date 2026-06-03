# Persistent worker pool for smtp_worker binary
# Keeps N long-running Go processes alive for the duration of a run
# Each worker handles jobs sequentially over its own stdin/stdout pipe
# No process fork per email

from __future__ import annotations

import asyncio
import json
import logging
from pathlib import Path

log = logging.getLogger("smtp_stinger.worker")

WORKER_BINARY: Path = Path(__file__).parent / "smtp_worker"


class _Worker:
    def __init__(self, worker_id: int):
        self.id = worker_id
        self._proc: asyncio.subprocess.Process | None = None
        self._lock = asyncio.Lock()

    async def start(self) -> None:
        self._proc = await asyncio.create_subprocess_exec(
            str(WORKER_BINARY),
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.DEVNULL,
        )
        log.debug("worker %d started (pid %d)", self.id, self._proc.pid)

    async def send(self, job: dict, hard_timeout: float) -> dict:
        assert self._proc and self._proc.stdin and self._proc.stdout

        line = json.dumps(job).encode() + b"\n"
        try:
            self._proc.stdin.write(line)
            await self._proc.stdin.drain()
        except (BrokenPipeError, ConnectionResetError) as e:
            raise _WorkerDead(f"stdin write failed: {e}")

        try:
            raw = await asyncio.wait_for(
                self._proc.stdout.readline(),
                timeout=hard_timeout,
            )
        except asyncio.TimeoutError:
            raise _WorkerDead(f"read timed out after {hard_timeout}s")

        if not raw:
            raise _WorkerDead("worker closed stdout unexpectedly")

        try:
            return json.loads(raw.decode("utf-8", errors="replace").strip())
        except json.JSONDecodeError as e:
            raise _WorkerDead(f"invalid JSON from worker: {e}")

    async def shutdown(self) -> None:
        if not self._proc:
            return
        try:
            if self._proc.returncode is None:
                line = json.dumps({"shutdown": True}).encode() + b"\n"
                self._proc.stdin.write(line)
                await self._proc.stdin.drain()
                await asyncio.wait_for(self._proc.wait(), timeout=3)
        except Exception:
            pass
        finally:
            if self._proc.returncode is None:
                self._proc.kill()
            self._proc = None
            log.debug("worker %d shut down", self.id)

    @property
    def alive(self) -> bool:
        return self._proc is not None and self._proc.returncode is None


class _WorkerDead(Exception):
    """Raised when a worker process is dead or unresponsive."""


class WorkerPool:
    # Pool of N persistent smtp_worker processes
    # Thread-safe via per-worker asyncio locks
    # Workers that die mid-run are automatically restarted

    def __init__(self, size: int, job_timeout_sec: int):
        self._size = size
        self._job_timeout = job_timeout_sec
        # connect + all SMTP commands + buffer, capped at 90s
        self._hard_timeout = min(job_timeout_sec * 3 + 10, 90)
        self._workers: list[_Worker] = []
        self._rr = 0
        self._rr_lock = asyncio.Lock()
        self._started = False

    async def start(self) -> None:
        if not WORKER_BINARY.exists():
            raise FileNotFoundError(
                f"smtp_worker binary not found at {WORKER_BINARY} — run: stinger build"
            )
        self._workers = [_Worker(i) for i in range(self._size)]
        await asyncio.gather(*[w.start() for w in self._workers])
        self._started = True
        log.debug("worker pool started (%d workers)", self._size)

    async def shutdown(self) -> None:
        if not self._started:
            return
        await asyncio.gather(*[w.shutdown() for w in self._workers], return_exceptions=True)
        self._workers.clear()
        self._started = False
        log.debug("worker pool shut down")

    async def call(self, job: dict) -> dict:
        for attempt in range(2):
            worker = await self._next_worker()
            async with worker._lock:
                if not worker.alive:
                    try:
                        await worker.start()
                    except OSError as e:
                        return _err(job, f"worker restart failed: {e}")

                try:
                    return await worker.send(job, self._hard_timeout)
                except _WorkerDead as e:
                    log.warning("worker %d died (%s), restarting", worker.id, e)
                    await worker.shutdown()
                    if attempt == 0:
                        # Restart and retry once
                        try:
                            await worker.start()
                            return await worker.send(job, self._hard_timeout)
                        except (_WorkerDead, OSError) as e2:
                            return _err(job, f"worker failed after restart: {e2}")
                    return _err(job, f"worker permanently failed: {e}")

        return _err(job, "worker dispatch failed after retries")

    async def _next_worker(self) -> _Worker:
        async with self._rr_lock:
            w = self._workers[self._rr % self._size]
            self._rr += 1
            return w

    async def __aenter__(self):
        await self.start()
        return self

    async def __aexit__(self, *_):
        await self.shutdown()


_pool: WorkerPool | None = None


def get_pool() -> WorkerPool:
    if _pool is None:
        raise RuntimeError("WorkerPool not initialised — call init_pool() first")
    return _pool


def init_pool(size: int, job_timeout_sec: int) -> WorkerPool:
    global _pool
    _pool = WorkerPool(size, job_timeout_sec)
    return _pool


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