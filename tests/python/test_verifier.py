# Tests for smtp_stinger.verifier

from __future__ import annotations

from unittest.mock import AsyncMock

import dns.exception
import dns.name
import dns.resolver
import pytest

from smtp_stinger.models import Status, SubStatus
from smtp_stinger.verifier import Verifier, _valid_email


class TestValidEmailRegex:
    @pytest.mark.parametrize("email", [
        "alice@example.com",
        "a.b+c@sub.example.co.uk",
        "user_name123@example.io",
    ])
    def test_accepts_valid_addresses(self, email):
        assert _valid_email(email)

    @pytest.mark.parametrize("email", [
        "not-an-email",
        "@example.com",
        "alice@",
        "alice@@example.com",
        "alice@.com",
        "",
    ])
    def test_rejects_invalid_addresses(self, email):
        assert not _valid_email(email)


class TestVerifyMalformed:
    @pytest.mark.asyncio
    async def test_no_at_symbol_is_invalid(self, base_cfg):
        v = Verifier(base_cfg)
        result = await v.verify("not-an-email")
        assert result.status == Status.INVALID
        assert result.sub_status == SubStatus.MALFORMED

    @pytest.mark.asyncio
    async def test_malformed_regex_fails_is_invalid(self, base_cfg):
        v = Verifier(base_cfg)
        result = await v.verify("@example.com")
        assert result.status == Status.INVALID
        assert result.sub_status == SubStatus.MALFORMED

    @pytest.mark.asyncio
    async def test_email_is_lowercased(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(side_effect=dns.resolver.NXDOMAIN())
        result = await v.verify("ALICE@EXAMPLE.COM")
        assert result.email == "alice@example.com"


class TestVerifyDNSErrors:
    @pytest.mark.asyncio
    async def test_nxdomain_is_invalid_no_mx(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(side_effect=dns.resolver.NXDOMAIN())
        result = await v.verify("a@nonexistent.invalid")
        assert result.status == Status.INVALID
        assert result.sub_status == SubStatus.NO_MX

    @pytest.mark.asyncio
    async def test_noanswer_is_invalid_no_mx(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(side_effect=dns.resolver.NoAnswer())
        result = await v.verify("a@example.com")
        assert result.status == Status.INVALID
        assert result.sub_status == SubStatus.NO_MX

    @pytest.mark.asyncio
    async def test_empty_label_is_invalid_malformed(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(side_effect=dns.name.EmptyLabel())
        result = await v.verify("a@.baddomain.com")
        assert result.status == Status.INVALID
        assert result.sub_status == SubStatus.MALFORMED

    @pytest.mark.asyncio
    async def test_no_nameservers_is_unknown_dns_error(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(side_effect=dns.resolver.NoNameservers())
        result = await v.verify("a@example.com")
        assert result.status == Status.UNKNOWN
        assert result.sub_status == SubStatus.DNS_ERROR

    @pytest.mark.asyncio
    async def test_timeout_is_unknown_dns_timeout(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(side_effect=dns.exception.Timeout())
        result = await v.verify("a@example.com")
        assert result.status == Status.UNKNOWN
        assert result.sub_status == SubStatus.DNS_TIMEOUT

    @pytest.mark.asyncio
    async def test_generic_dns_exception_is_unknown_dns_error(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(side_effect=dns.exception.DNSException("weird"))
        result = await v.verify("a@example.com")
        assert result.status == Status.UNKNOWN
        assert result.sub_status == SubStatus.DNS_ERROR

    @pytest.mark.asyncio
    async def test_empty_mx_list_is_invalid_no_mx(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(return_value=[])
        result = await v.verify("a@example.com")
        assert result.status == Status.INVALID
        assert result.sub_status == SubStatus.NO_MX


class TestVerifySMTPFlow:
    @pytest.mark.asyncio
    async def test_valid_smtp_response(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(return_value=["mx1.example.com"])
        v.dns.get_catch_all = lambda d: False  # not catch-all, already known
        v._smtp_once = AsyncMock(
            return_value={"smtp_code": 250, "smtp_message": "OK", "tls_used": True, "duration_ms": 50}
        )
        result = await v.verify("a@example.com")
        assert result.status == Status.VALID
        assert result.sub_status == SubStatus.CONFIRMED
        assert result.mx_used == "mx1.example.com"

    @pytest.mark.asyncio
    async def test_invalid_smtp_response_stops_immediately(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(return_value=["mx1.example.com", "mx2.example.com"])
        v.dns.get_catch_all = lambda d: False
        call_count = 0

        async def fake_smtp(email, mx):
            nonlocal call_count
            call_count += 1
            return {"smtp_code": 550, "smtp_message": "no such user"}

        v._smtp_once = fake_smtp
        result = await v.verify("a@example.com")
        assert result.status == Status.INVALID
        assert result.sub_status == SubStatus.MAILBOX_NOT_FOUND
        # Should stop at first MX — no retry across MX for permanent rejection
        assert call_count == 1

    @pytest.mark.asyncio
    async def test_catch_all_domain_marks_valid_as_catch_all(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(return_value=["mx1.example.com"])
        v.dns.get_catch_all = lambda d: True  # already known catch-all
        v._smtp_once = AsyncMock(
            return_value={"smtp_code": 250, "smtp_message": "OK", "tls_used": False, "duration_ms": 30}
        )
        result = await v.verify("a@example.com")
        assert result.status == Status.CATCH_ALL
        assert result.sub_status == SubStatus.CATCH_ALL
        assert result.is_catch_all_domain is True

    @pytest.mark.asyncio
    async def test_retries_on_transient_then_exhausts(self, base_cfg):
        base_cfg["retry"]["max_attempts"] = 2
        base_cfg["retry"]["backoff_base_sec"] = 0  # no real sleep in tests
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(return_value=["mx1.example.com"])
        v.dns.get_catch_all = lambda d: False
        v._smtp_once = AsyncMock(
            return_value={"smtp_code": 451, "smtp_message": "greylisted"}
        )
        result = await v.verify("a@example.com")
        assert result.status == Status.UNKNOWN
        assert result.sub_status == SubStatus.GREYLISTED
        # max_attempts=2, 1 mx -> 2 total calls
        assert v._smtp_once.call_count == 2

    @pytest.mark.asyncio
    async def test_falls_back_to_second_mx_on_transient(self, base_cfg):
        base_cfg["retry"]["max_attempts"] = 1
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(return_value=["mx1.example.com", "mx2.example.com"])
        v.dns.get_catch_all = lambda d: False

        responses = [
            {"smtp_code": 421, "smtp_message": "busy"},
            {"smtp_code": 250, "smtp_message": "OK", "tls_used": False, "duration_ms": 10},
        ]
        call_log = []

        async def fake_smtp(email, mx):
            call_log.append(mx)
            return responses[len(call_log) - 1]

        v._smtp_once = fake_smtp
        result = await v.verify("a@example.com")
        assert result.status == Status.VALID
        assert call_log == ["mx1.example.com", "mx2.example.com"]


class TestSmokeTestDNS:
    @pytest.mark.asyncio
    async def test_returns_none_when_gmail_mx_found(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(return_value=["gmail-smtp-in.l.google.com"])
        err = await v.smoke_test_dns()
        assert err is None

    @pytest.mark.asyncio
    async def test_returns_error_string_when_no_mx(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(return_value=[])
        err = await v.smoke_test_dns()
        assert err is not None
        assert "gmail.com" in err

    @pytest.mark.asyncio
    async def test_returns_error_string_on_exception(self, base_cfg):
        v = Verifier(base_cfg)
        v.dns.get_mx = AsyncMock(side_effect=dns.resolver.NoNameservers())
        err = await v.smoke_test_dns()
        assert err is not None
        assert "NoNameservers" in err


class TestTick:
    @pytest.mark.asyncio
    async def test_tick_increments_done(self, base_cfg):
        v = Verifier(base_cfg)
        assert v.done == 0
        await v.tick()
        await v.tick()
        assert v.done == 2