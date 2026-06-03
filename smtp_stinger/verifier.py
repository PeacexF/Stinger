# Core verifier — orchestrates DNS, catch-all detection, retries, concurrency

from __future__ import annotations

import asyncio
import re
import time
from collections import defaultdict

import dns.exception
import dns.name
import dns.resolver

from .dns_cache import DNSCache
from .models import EmailResult, Status, SubStatus, classify_worker_result
from .worker import build_job, get_pool

CATCH_ALL_PROBE_PREFIX = "stinger-catch_all-checker-XYZ"

_EMAIL_RE = re.compile(r"^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$")


def _valid_email(email: str) -> bool:
    return bool(_EMAIL_RE.match(email))


class Verifier:
    def __init__(self, cfg: dict):
        self.cfg = cfg
        self.smtp_cfg = cfg["smtp"]
        self.dns = DNSCache(cfg)
        # Global semaphore still controls max concurrent in-flight jobs
        # so we don't overwhelm the pool with more work than it can process
        self.global_sem = asyncio.Semaphore(cfg["concurrency"]["global_limit"])
        self._domain_sems: dict[str, asyncio.Semaphore] = defaultdict(
            lambda: asyncio.Semaphore(cfg["concurrency"]["per_domain_limit"])
        )
        self._catch_all_locks: dict[str, asyncio.Lock] = defaultdict(asyncio.Lock)
        self.max_attempts = cfg["retry"]["max_attempts"]
        self.backoff_base = cfg["retry"]["backoff_base_sec"]
        self.total: int = 0
        self.done: int = 0
        self._progress_lock = asyncio.Lock()

    async def smoke_test_dns(self) -> str | None:
        # resolve gmail.com MX as a quick sanity check before the main run
        try:
            mxs = await self.dns.get_mx("gmail.com")
            if not mxs:
                return (
                    "DNS smoke test failed: gmail.com returned no MX records. "
                    "Check your resolvers in config.yaml."
                )
            return None
        except Exception as e:
            return (
                f"DNS smoke test failed: {type(e).__name__}: {e}. "
                "Check your resolvers in config.yaml."
            )

    async def verify(self, email: str) -> EmailResult:
        email = email.strip().lower()

        if "@" not in email or not _valid_email(email):
            return EmailResult(
                email=email,
                status=Status.INVALID,
                sub_status=SubStatus.MALFORMED,
                reason="malformed email address",
            )

        domain = email.split("@")[1]

        try:
            mxs = await self.dns.get_mx(domain)
        except (dns.resolver.NXDOMAIN, dns.resolver.NoAnswer):
            return EmailResult(
                email=email,
                status=Status.INVALID,
                sub_status=SubStatus.NO_MX,
                reason="no MX records (NXDOMAIN or no answer)",
            )
        except dns.name.EmptyLabel:
            return EmailResult(
                email=email,
                status=Status.INVALID,
                sub_status=SubStatus.MALFORMED,
                reason="malformed domain (empty label)",
            )
        except dns.resolver.NoNameservers:
            return EmailResult(
                email=email,
                status=Status.UNKNOWN,
                sub_status=SubStatus.DNS_ERROR,
                reason="DNS lookup failed: all nameservers refused to answer",
            )
        except dns.exception.Timeout:
            return EmailResult(
                email=email,
                status=Status.UNKNOWN,
                sub_status=SubStatus.DNS_TIMEOUT,
                reason="DNS lookup timed out",
            )
        except dns.exception.DNSException as e:
            return EmailResult(
                email=email,
                status=Status.UNKNOWN,
                sub_status=SubStatus.DNS_ERROR,
                reason=f"DNS error: {type(e).__name__}: {e}",
            )

        if not mxs:
            return EmailResult(
                email=email,
                status=Status.INVALID,
                sub_status=SubStatus.NO_MX,
                reason="no MX records (NXDOMAIN or no answer)",
            )

        is_catch_all = await self._detect_catch_all(domain, mxs)
        return await self._probe_with_retry(email, domain, mxs, is_catch_all)

    async def tick(self) -> None:
        async with self._progress_lock:
            self.done += 1


    async def _detect_catch_all(self, domain: str, mxs: list[str]) -> bool:
        cached = self.dns.get_catch_all(domain)
        if cached is not None:
            return cached

        async with self._catch_all_locks[domain]:
            cached = self.dns.get_catch_all(domain)
            if cached is not None:
                return cached

            probe = f"{CATCH_ALL_PROBE_PREFIX}{int(time.monotonic() * 1000)}@{domain}"
            for mx in mxs[:2]:
                res = await self._smtp_once(probe, mx)
                code = res.get("smtp_code", 0)
                if code in {250, 251}:
                    self.dns.set_catch_all(domain, True)
                    return True
                if code >= 500 or (code == 0 and not res.get("error")):
                    self.dns.set_catch_all(domain, False)
                    return False

            self.dns.set_catch_all(domain, False)
            return False


    async def _smtp_once(self, email: str, mx: str) -> dict:
        domain = email.split("@")[1]
        async with self.global_sem:
            async with self._domain_sems[domain]:
                return await get_pool().call(build_job(email, mx, self.smtp_cfg))

    async def _probe_with_retry(
        self, email: str, domain: str, mxs: list[str], is_catch_all: bool
    ) -> EmailResult:
        attempts = 0
        last_res: dict = {}
        last_status = Status.UNKNOWN
        last_sub_status = SubStatus.TEMP_FAILURE
        last_reason = ""

        for attempt in range(self.max_attempts):
            for mx in mxs:
                attempts += 1
                res = await self._smtp_once(email, mx)
                status, sub_status, reason = classify_worker_result(res)

                if status == Status.VALID:
                    return EmailResult(
                        email=email,
                        status=Status.CATCH_ALL if is_catch_all else Status.VALID,
                        sub_status=SubStatus.CATCH_ALL if is_catch_all else sub_status,
                        smtp_code=res.get("smtp_code"),
                        smtp_message=res.get("smtp_message"),
                        mx_used=mx,
                        tls_used=res.get("tls_used", False),
                        is_catch_all_domain=is_catch_all,
                        attempts=attempts,
                        duration_ms=res.get("duration_ms", 0),
                        reason=reason,
                    )

                if status == Status.INVALID:
                    return EmailResult(
                        email=email,
                        status=Status.INVALID,
                        sub_status=sub_status,
                        smtp_code=res.get("smtp_code"),
                        smtp_message=res.get("smtp_message"),
                        mx_used=mx,
                        tls_used=res.get("tls_used", False),
                        is_catch_all_domain=is_catch_all,
                        attempts=attempts,
                        duration_ms=res.get("duration_ms", 0),
                        reason=reason,
                    )

                last_res, last_status, last_sub_status, last_reason = (
                    res, status, sub_status, reason
                )

            if attempt < self.max_attempts - 1:
                await asyncio.sleep(self.backoff_base * (2 ** attempt))

        return EmailResult(
            email=email,
            status=last_status,
            sub_status=last_sub_status,
            smtp_code=last_res.get("smtp_code"),
            smtp_message=last_res.get("smtp_message"),
            mx_used=mxs[0] if mxs else None,
            tls_used=last_res.get("tls_used", False),
            is_catch_all_domain=is_catch_all,
            attempts=attempts,
            duration_ms=last_res.get("duration_ms", 0),
            reason=last_reason,
        )