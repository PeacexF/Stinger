# Tests for models

from __future__ import annotations

import json

import pytest

from smtp_stinger.models import (
    EmailResult,
    Status,
    SubStatus,
    classify_worker_result,
)


class TestClassifyWorkerResultValid:
    def test_250_is_valid(self):
        status, sub, reason = classify_worker_result(
            {"smtp_code": 250, "smtp_message": "2.1.5 OK"}
        )
        assert status == Status.VALID
        assert sub == SubStatus.CONFIRMED
        assert reason == "2.1.5 OK"

    def test_251_is_valid(self):
        status, sub, _ = classify_worker_result({"smtp_code": 251, "smtp_message": "forwarding"})
        assert status == Status.VALID
        assert sub == SubStatus.CONFIRMED


class TestClassifyWorkerResultInvalid:
    @pytest.mark.parametrize("code", [550, 551])
    def test_mailbox_not_found(self, code):
        status, sub, reason = classify_worker_result(
            {"smtp_code": code, "smtp_message": "user unknown"}
        )
        assert status == Status.INVALID
        assert sub == SubStatus.MAILBOX_NOT_FOUND
        assert "user unknown" in reason

    def test_552_mailbox_full(self):
        status, sub, _ = classify_worker_result({"smtp_code": 552, "smtp_message": "quota"})
        assert status == Status.INVALID
        assert sub == SubStatus.MAILBOX_FULL

    def test_553_domain_rejected(self):
        status, sub, _ = classify_worker_result({"smtp_code": 553, "smtp_message": "bad addr"})
        assert status == Status.INVALID
        assert sub == SubStatus.DOMAIN_REJECTED

    def test_554_with_spam_keyword_is_spam_block(self):
        status, sub, _ = classify_worker_result(
            {"smtp_code": 554, "smtp_message": "rejected due to spam policy"}
        )
        assert status == Status.INVALID
        assert sub == SubStatus.SPAM_BLOCK

    def test_554_with_blacklist_keyword_is_spam_block(self):
        status, sub, _ = classify_worker_result(
            {"smtp_code": 554, "smtp_message": "IP on blacklist"}
        )
        assert sub == SubStatus.SPAM_BLOCK

    def test_554_without_spam_keyword_is_domain_rejected(self):
        status, sub, _ = classify_worker_result(
            {"smtp_code": 554, "smtp_message": "transaction failed"}
        )
        assert status == Status.INVALID
        assert sub == SubStatus.DOMAIN_REJECTED

    def test_501_syntax_error(self):
        status, sub, _ = classify_worker_result({"smtp_code": 501, "smtp_message": "bad syntax"})
        assert status == Status.INVALID
        assert sub == SubStatus.SYNTAX_ERROR


class TestClassifyWorkerResultRetryable:
    def test_421_rate_limited(self):
        status, sub, _ = classify_worker_result({"smtp_code": 421, "smtp_message": "too many"})
        assert status == Status.UNKNOWN
        assert sub == SubStatus.RATE_LIMITED

    def test_450_mailbox_temp(self):
        status, sub, _ = classify_worker_result({"smtp_code": 450, "smtp_message": "busy"})
        assert status == Status.UNKNOWN
        assert sub == SubStatus.MAILBOX_TEMP

    def test_451_greylisted(self):
        status, sub, _ = classify_worker_result({"smtp_code": 451, "smtp_message": "try later"})
        assert status == Status.UNKNOWN
        assert sub == SubStatus.GREYLISTED

    def test_452_mailbox_full_temp(self):
        status, sub, _ = classify_worker_result({"smtp_code": 452, "smtp_message": "temp full"})
        assert status == Status.UNKNOWN
        assert sub == SubStatus.MAILBOX_FULL

    def test_503_sequence_error(self):
        status, sub, _ = classify_worker_result({"smtp_code": 503, "smtp_message": "bad sequence"})
        assert status == Status.UNKNOWN
        assert sub == SubStatus.TEMP_FAILURE

    def test_generic_4xx_temp_failure(self):
        status, sub, _ = classify_worker_result({"smtp_code": 432, "smtp_message": "?"})
        assert status == Status.UNKNOWN
        assert sub == SubStatus.TEMP_FAILURE


class TestClassifyWorkerResultNetworkErrors:
    def test_timeout_error(self):
        status, sub, reason = classify_worker_result(
            {"smtp_code": 0, "error": "connect timeout after 10s"}
        )
        assert status == Status.UNKNOWN
        assert sub == SubStatus.CONNECT_FAILED
        assert "timed out" in reason

    def test_connection_refused(self):
        status, sub, _ = classify_worker_result(
            {"smtp_code": 0, "error": "connect failed: connection refused"}
        )
        assert status == Status.UNKNOWN
        assert sub == SubStatus.CONNECT_FAILED

    def test_worker_specific_error(self):
        status, sub, _ = classify_worker_result(
            {"smtp_code": 0, "error": "worker process crashed"}
        )
        assert status == Status.UNKNOWN
        assert sub == SubStatus.WORKER_ERROR

    def test_generic_network_error(self):
        status, sub, _ = classify_worker_result(
            {"smtp_code": 0, "error": "some unexpected network issue"}
        )
        assert status == Status.UNKNOWN
        assert sub == SubStatus.CONNECT_FAILED

    def test_empty_result_is_unclassified(self):
        status, sub, reason = classify_worker_result({})
        assert status == Status.UNKNOWN
        assert "unclassified" in reason


class TestEmailResultSerialization:
    def test_to_jsonl_basic_fields(self):
        result = EmailResult(
            email="alice@example.com",
            status=Status.VALID,
            sub_status=SubStatus.CONFIRMED,
            smtp_code=250,
            smtp_message="OK",
        )
        data = json.loads(result.to_jsonl())
        assert data["email"] == "alice@example.com"
        assert data["status"] == "valid"
        assert data["sub_status"] == "confirmed"
        assert data["smtp_code"] == 250

    def test_to_jsonl_handles_none_sub_status(self):
        result = EmailResult(email="bob@example.com", status=Status.INVALID)
        data = json.loads(result.to_jsonl())
        assert data["sub_status"] is None

    def test_to_jsonl_includes_timestamp(self):
        result = EmailResult(email="x@example.com", status=Status.UNKNOWN)
        data = json.loads(result.to_jsonl())
        assert "timestamp" in data
        assert "T" in data["timestamp"]  # ISO format marker

    def test_default_values(self):
        result = EmailResult(email="x@example.com", status=Status.VALID)
        assert result.attempts == 0
        assert result.duration_ms == 0
        assert result.tls_used is False
        assert result.is_catch_all_domain is False