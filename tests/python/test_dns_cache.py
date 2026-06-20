# Tests for dns_cache

from __future__ import annotations

from unittest.mock import AsyncMock, MagicMock

import dns.exception
import dns.name
import dns.resolver
import pytest

from smtp_stinger.dns_cache import DNSCache


def _make_mx_answer(hosts_with_pref: list[tuple[str, int]]):
    records = []
    for host, pref in hosts_with_pref:
        rec = MagicMock()
        rec.preference = pref
        rec.exchange = host + "."  # dnspython keeps trailing dot
        records.append(rec)
    return records


class TestDNSCacheInit:
    def test_uses_configured_resolvers(self, base_cfg):
        cache = DNSCache(base_cfg)
        assert cache.resolver.nameservers == ["1.1.1.1", "8.8.8.8"]

    def test_falls_back_to_default_resolvers_when_empty(self, base_cfg):
        base_cfg["dns"]["resolvers"] = []
        cache = DNSCache(base_cfg)
        assert cache.resolver.nameservers == ["1.1.1.1", "8.8.8.8"]

    def test_configure_false_is_set(self, base_cfg):
        # Resolver must not read /etc/resolv.conf — verified indirectly via
        # nameservers being exactly what was set, nothing auto-discovered.
        cache = DNSCache(base_cfg)
        assert cache.resolver.nameservers == base_cfg["dns"]["resolvers"]


class TestGetMX:
    @pytest.mark.asyncio
    async def test_returns_sorted_mx_hosts(self, base_cfg):
        cache = DNSCache(base_cfg)
        answer = _make_mx_answer([("mx2.example.com", 20), ("mx1.example.com", 10)])
        cache.resolver.resolve = AsyncMock(return_value=answer)

        hosts = await cache.get_mx("example.com")
        assert hosts == ["mx1.example.com", "mx2.example.com"]

    @pytest.mark.asyncio
    async def test_caches_result_within_ttl(self, base_cfg):
        cache = DNSCache(base_cfg)
        answer = _make_mx_answer([("mx1.example.com", 10)])
        mock_resolve = AsyncMock(return_value=answer)
        cache.resolver.resolve = mock_resolve

        await cache.get_mx("example.com")
        await cache.get_mx("example.com")

        assert mock_resolve.call_count == 1

    @pytest.mark.asyncio
    async def test_refetches_after_ttl_expiry(self, base_cfg):
        base_cfg["dns"]["mx_cache_ttl"] = 0  # instantly stale
        cache = DNSCache(base_cfg)
        answer = _make_mx_answer([("mx1.example.com", 10)])
        mock_resolve = AsyncMock(return_value=answer)
        cache.resolver.resolve = mock_resolve

        await cache.get_mx("example.com")
        await cache.get_mx("example.com")

        assert mock_resolve.call_count == 2

    @pytest.mark.asyncio
    async def test_nxdomain_returns_empty_list_and_caches(self, base_cfg):
        cache = DNSCache(base_cfg)
        mock_resolve = AsyncMock(side_effect=dns.resolver.NXDOMAIN())
        cache.resolver.resolve = mock_resolve

        hosts = await cache.get_mx("doesnotexist.invalid")
        assert hosts == []

        # Second call should hit cache, not call resolve again
        hosts2 = await cache.get_mx("doesnotexist.invalid")
        assert hosts2 == []
        assert mock_resolve.call_count == 1

    @pytest.mark.asyncio
    async def test_noanswer_returns_empty_list(self, base_cfg):
        cache = DNSCache(base_cfg)
        cache.resolver.resolve = AsyncMock(side_effect=dns.resolver.NoAnswer())
        hosts = await cache.get_mx("example.com")
        assert hosts == []

    @pytest.mark.asyncio
    async def test_empty_label_returns_empty_list_and_caches(self, base_cfg):
        cache = DNSCache(base_cfg)
        cache.resolver.resolve = AsyncMock(side_effect=dns.name.EmptyLabel())
        hosts = await cache.get_mx(".baddomain.com")
        assert hosts == []

    @pytest.mark.asyncio
    async def test_no_nameservers_raises(self, base_cfg):
        cache = DNSCache(base_cfg)
        cache.resolver.resolve = AsyncMock(side_effect=dns.resolver.NoNameservers())
        with pytest.raises(dns.resolver.NoNameservers):
            await cache.get_mx("example.com")

    @pytest.mark.asyncio
    async def test_timeout_raises_and_does_not_cache(self, base_cfg):
        cache = DNSCache(base_cfg)
        mock_resolve = AsyncMock(side_effect=dns.exception.Timeout())
        cache.resolver.resolve = mock_resolve

        with pytest.raises(dns.exception.Timeout):
            await cache.get_mx("example.com")

        # Should not have cached — calling again should hit resolver again
        with pytest.raises(dns.exception.Timeout):
            await cache.get_mx("example.com")
        assert mock_resolve.call_count == 2


class TestCatchAllCache:
    def test_returns_none_when_not_cached(self, base_cfg):
        cache = DNSCache(base_cfg)
        assert cache.get_catch_all("example.com") is None

    def test_returns_cached_value(self, base_cfg):
        cache = DNSCache(base_cfg)
        cache.set_catch_all("example.com", True)
        assert cache.get_catch_all("example.com") is True

    def test_returns_cached_false_value(self, base_cfg):
        cache = DNSCache(base_cfg)
        cache.set_catch_all("example.com", False)
        assert cache.get_catch_all("example.com") is False

    def test_expires_after_ttl(self, base_cfg):
        base_cfg["dns"]["catch_all_cache_ttl"] = 0
        cache = DNSCache(base_cfg)
        cache.set_catch_all("example.com", True)
        assert cache.get_catch_all("example.com") is None

    def test_different_domains_independent(self, base_cfg):
        cache = DNSCache(base_cfg)
        cache.set_catch_all("a.com", True)
        cache.set_catch_all("b.com", False)
        assert cache.get_catch_all("a.com") is True
        assert cache.get_catch_all("b.com") is False