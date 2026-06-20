# Tests for worker

from __future__ import annotations

import asyncio
import json
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from smtp_stinger.worker import (
    WorkerPool,
    _err,
    _Worker,
    _WorkerDead,
    build_job,
    get_pool,
    init_pool,
)


def make_fake_proc(responses: list[bytes], returncode=None):
    proc = MagicMock()
    proc.returncode = returncode
    proc.pid = 1234

    proc.stdin = MagicMock()
    proc.stdin.write = MagicMock()
    proc.stdin.drain = AsyncMock()

    proc.stdout = MagicMock()
    iterator = iter(responses)

    async def fake_readline():
        try:
            return next(iterator)
        except StopIteration:
            return b""

    proc.stdout.readline = fake_readline
    proc.wait = AsyncMock(return_value=0)
    proc.kill = MagicMock()
    return proc


class TestBuildJob:
    def test_builds_correct_job_dict(self):
        smtp_cfg = {
            "helo_hostname": "mail.example.com",
            "mail_from": "verify@example.com",
            "connect_timeout_sec": 8,
            "try_tls": True,
            "port": 25,
        }
        job = build_job("alice@target.com", "mx1.target.com", smtp_cfg)
        assert job["email"] == "alice@target.com"
        assert job["mx"] == "mx1.target.com"
        assert job["helo"] == "mail.example.com"
        assert job["mail_from"] == "verify@example.com"
        assert job["timeout_sec"] == 8
        assert job["port"] == 25

    def test_uses_defaults_for_missing_optional_fields(self):
        smtp_cfg = {"helo_hostname": "mail.example.com", "mail_from": "v@example.com"}
        job = build_job("a@b.com", "mx.b.com", smtp_cfg)
        assert job["timeout_sec"] == 10
        assert job["try_tls"] is True
        assert job["port"] == 25


class TestErrHelper:
    def test_err_includes_email_and_mx_from_job(self):
        job = {"email": "a@b.com", "mx": "mx.b.com"}
        result = _err(job, "something broke")
        assert result["email"] == "a@b.com"
        assert result["mx"] == "mx.b.com"
        assert result["error"] == "something broke"
        assert result["smtp_code"] == 0

    def test_err_handles_missing_keys(self):
        result = _err({}, "broke")
        assert result["email"] == ""
        assert result["mx"] == ""


class TestWorkerSend:
    @pytest.mark.asyncio
    async def test_send_returns_parsed_json(self):
        response = {"email": "a@b.com", "smtp_code": 250, "smtp_message": "OK"}
        proc = make_fake_proc([json.dumps(response).encode() + b"\n"])

        worker = _Worker(0)
        worker._proc = proc

        result = await worker.send({"email": "a@b.com"}, hard_timeout=5)
        assert result["smtp_code"] == 250

    @pytest.mark.asyncio
    async def test_send_raises_worker_dead_on_empty_readline(self):
        proc = make_fake_proc([b""])  # simulates closed stdout
        worker = _Worker(0)
        worker._proc = proc

        with pytest.raises(_WorkerDead, match="closed stdout"):
            await worker.send({"email": "a@b.com"}, hard_timeout=5)

    @pytest.mark.asyncio
    async def test_send_raises_worker_dead_on_invalid_json(self):
        proc = make_fake_proc([b"not valid json{{{\n"])
        worker = _Worker(0)
        worker._proc = proc

        with pytest.raises(_WorkerDead, match="invalid JSON"):
            await worker.send({"email": "a@b.com"}, hard_timeout=5)

    @pytest.mark.asyncio
    async def test_send_raises_worker_dead_on_broken_pipe(self):
        proc = make_fake_proc([b"{}\n"])
        proc.stdin.write = MagicMock(side_effect=BrokenPipeError("pipe broke"))
        worker = _Worker(0)
        worker._proc = proc

        with pytest.raises(_WorkerDead, match="stdin write failed"):
            await worker.send({"email": "a@b.com"}, hard_timeout=5)

    @pytest.mark.asyncio
    async def test_send_raises_worker_dead_on_timeout(self):
        proc = make_fake_proc([])

        async def hang_forever():
            await asyncio.sleep(100)

        proc.stdout.readline = hang_forever
        worker = _Worker(0)
        worker._proc = proc

        with pytest.raises(_WorkerDead, match="timed out"):
            await worker.send({"email": "a@b.com"}, hard_timeout=0.01)


class TestWorkerAlive:
    def test_alive_false_when_no_process(self):
        worker = _Worker(0)
        assert worker.alive is False

    def test_alive_true_when_process_running(self):
        worker = _Worker(0)
        worker._proc = make_fake_proc([], returncode=None)
        assert worker.alive is True

    def test_alive_false_when_process_exited(self):
        worker = _Worker(0)
        worker._proc = make_fake_proc([], returncode=1)
        assert worker.alive is False


class TestWorkerPoolDispatch:
    @pytest.mark.asyncio
    async def test_call_dispatches_to_alive_worker(self):
        pool = WorkerPool(size=2, job_timeout_sec=5)
        response = json.dumps({"smtp_code": 250}).encode() + b"\n"

        w0 = _Worker(0)
        w0._proc = make_fake_proc([response])
        w1 = _Worker(1)
        w1._proc = make_fake_proc([response])
        pool._workers = [w0, w1]
        pool._started = True

        result = await pool.call({"email": "a@b.com", "mx": "mx.b.com"})
        assert result["smtp_code"] == 250

    @pytest.mark.asyncio
    async def test_round_robin_distributes_across_workers(self):
        pool = WorkerPool(size=3, job_timeout_sec=5)
        response = json.dumps({"smtp_code": 250}).encode() + b"\n"

        workers = []
        for i in range(3):
            w = _Worker(i)
            # each worker can answer multiple times
            w._proc = make_fake_proc([response, response])
            workers.append(w)
        pool._workers = workers
        pool._started = True

        seen_ids = []
        for _ in range(3):
            w = await pool._next_worker()
            seen_ids.append(w.id)

        assert seen_ids == [0, 1, 2]

    @pytest.mark.asyncio
    async def test_dead_worker_is_restarted_and_retried(self):
        pool = WorkerPool(size=1, job_timeout_sec=5)
        w0 = _Worker(0)
        # First proc: dies immediately (empty readline)
        w0._proc = make_fake_proc([b""])
        pool._workers = [w0]
        pool._started = True

        good_response = json.dumps({"smtp_code": 250}).encode() + b"\n"

        async def fake_start():
            w0._proc = make_fake_proc([good_response])

        with patch.object(w0, "start", side_effect=fake_start):
            result = await pool.call({"email": "a@b.com", "mx": "mx.b.com"})

        assert result["smtp_code"] == 250

    @pytest.mark.asyncio
    async def test_permanently_dead_worker_returns_error_result(self):
        pool = WorkerPool(size=1, job_timeout_sec=5)
        w0 = _Worker(0)
        w0._proc = make_fake_proc([b""])  # always dies
        pool._workers = [w0]
        pool._started = True

        async def always_dies():
            w0._proc = make_fake_proc([b""])

        with patch.object(w0, "start", side_effect=always_dies):
            result = await pool.call({"email": "a@b.com", "mx": "mx.b.com"})

        assert result["smtp_code"] == 0
        assert "worker" in result["error"].lower()


class TestWorkerPoolLifecycle:
    @pytest.mark.asyncio
    async def test_start_raises_if_binary_missing(self, tmp_path, monkeypatch):
        import smtp_stinger.worker as worker_mod
        monkeypatch.setattr(worker_mod, "WORKER_BINARY", tmp_path / "nonexistent_binary")

        pool = WorkerPool(size=2, job_timeout_sec=5)
        with pytest.raises(FileNotFoundError, match="stinger build"):
            await pool.start()

    @pytest.mark.asyncio
    async def test_shutdown_noop_when_not_started(self):
        pool = WorkerPool(size=2, job_timeout_sec=5)
        # Should not raise even though start() was never called
        await pool.shutdown()


class TestGetPoolInitPool:
    def test_get_pool_raises_before_init(self):
        import smtp_stinger.worker as worker_mod
        worker_mod._pool = None
        with pytest.raises(RuntimeError, match="not initialised"):
            get_pool()

    def test_init_pool_sets_module_global(self):
        pool = init_pool(size=4, job_timeout_sec=10)
        assert get_pool() is pool
        assert pool._size == 4