# DNS resolver module
# catch-all caching

from __future__ import annotations

import time
from typing import Optional

import dns.asyncresolver
import dns.exception
import dns.name
import dns.resolver


class DNSCache:
    def __init__(self, cfg: dict):
        self._mx: dict[str, list[str]] = {}
        self._mx_ts: dict[str, float] = {}
        self._catch_all: dict[str, bool] = {}
        self._catch_all_ts: dict[str, float] = {}
        self._mx_ttl = cfg["dns"]["mx_cache_ttl"]
        self._ca_ttl = cfg["dns"]["catch_all_cache_ttl"]

        self.resolver = dns.asyncresolver.Resolver(configure=False)
        resolvers = cfg["dns"].get("resolvers") or []
        if resolvers:
            self.resolver.nameservers = resolvers
        else:
            self.resolver.nameservers = ["1.1.1.1", "8.8.8.8"]

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
        except (dns.resolver.NXDOMAIN, dns.resolver.NoAnswer):
            # Permanent — domain exists but has no MX, or doesn't exist at all
            self._mx[domain] = []
            self._mx_ts[domain] = time.monotonic()
            return []
        except dns.resolver.NoNameservers:
            # All nameservers failed — transient, don't cache
            raise
        except dns.name.EmptyLabel:
            # Malformed domain (e.g. ".bk.ru") — permanent, cache as empty
            self._mx[domain] = []
            self._mx_ts[domain] = time.monotonic()
            return []
        except dns.exception.Timeout:
            raise
        except dns.exception.DNSException:
            raise

    def get_catch_all(self, domain: str) -> Optional[bool]:
        if self._stale(self._catch_all_ts, domain, self._ca_ttl):
            return None
        return self._catch_all.get(domain)

    def set_catch_all(self, domain: str, value: bool) -> None:
        self._catch_all[domain] = value
        self._catch_all_ts[domain] = time.monotonic()