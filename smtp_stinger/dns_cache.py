# DNS resolver module
# catch-all caching


from __future__ import annotations

import time
from typing import Optional

import dns.asyncresolver
import dns.exception
import dns.resolver


class DNSCache:
    def __init__(self, cfg: dict):
        self._mx: dict[str, list[str]] = {}
        self._mx_ts: dict[str, float] = {}
        self._catch_all: dict[str, bool] = {}
        self._catch_all_ts: dict[str, float] = {}
        self._mx_ttl = cfg["dns"]["mx_cache_ttl"]
        self._ca_ttl = cfg["dns"]["catch_all_cache_ttl"]

        self.resolver = dns.asyncresolver.Resolver()
        resolvers = cfg["dns"].get("resolvers") or []
        if resolvers:
            self.resolver.nameservers = resolvers

    def _stale(self, ts_map: dict, key: str, ttl: int) -> bool:
        return key not in ts_map or (time.monotonic() - ts_map[key]) > ttl

    async def get_mx(self, domain: str) -> list[str]:
        if not self._stale(self._mx_ts, domain, self._mx_ttl):
            return self._mx.get(domain, [])
        try:
            answers = await self.resolver.resolve(domain, "MX")
            hosts = [
                str(r.exchange).rstrip(".")
                for r in sorted(answers, key=lambda r: r.preference)
            ]
            self._mx[domain] = hosts
            self._mx_ts[domain] = time.monotonic()
            return hosts
        except (
            dns.exception.NXDOMAIN,
            dns.exception.NoAnswer,
            dns.resolver.NoNameservers,
        ):
            self._mx[domain] = []
            self._mx_ts[domain] = time.monotonic()
            return []
        except dns.exception.DNSException:
            # Transient failure — don't cache, let caller handle retry
            raise

    def get_catch_all(self, domain: str) -> Optional[bool]:
        if self._stale(self._catch_all_ts, domain, self._ca_ttl):
            return None
        return self._catch_all.get(domain)

    def set_catch_all(self, domain: str, value: bool) -> None:
        self._catch_all[domain] = value
        self._catch_all_ts[domain] = time.monotonic()