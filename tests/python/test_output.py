# Tests for output

from __future__ import annotations

from smtp_stinger.models import EmailResult, Status, SubStatus
from smtp_stinger.output import ResultWriter, summarise_jsonl


def _result(email, status, sub_status=None, **kw) -> EmailResult:
    return EmailResult(email=email, status=status, sub_status=sub_status, **kw)


class TestResultWriter:
    def test_writes_jsonl_for_every_result(self, base_cfg, tmp_path):
        base_cfg["output"]["output_dir"] = str(tmp_path)
        with ResultWriter(base_cfg) as w:
            w.write(_result("a@b.com", Status.VALID, SubStatus.CONFIRMED))
            w.write(_result("c@d.com", Status.INVALID, SubStatus.MAILBOX_NOT_FOUND))

        lines = (tmp_path / "results.jsonl").read_text().splitlines()
        assert len(lines) == 2

    def test_only_valid_and_catch_all_go_to_valid_txt(self, base_cfg, tmp_path):
        base_cfg["output"]["output_dir"] = str(tmp_path)
        with ResultWriter(base_cfg) as w:
            w.write(_result("valid@b.com", Status.VALID, SubStatus.CONFIRMED))
            w.write(_result("catchall@b.com", Status.CATCH_ALL, SubStatus.CATCH_ALL))
            w.write(_result("invalid@b.com", Status.INVALID, SubStatus.MAILBOX_NOT_FOUND))
            w.write(_result("unknown@b.com", Status.UNKNOWN, SubStatus.CONNECT_FAILED))

        valid_emails = (tmp_path / "valid_emails.txt").read_text().splitlines()
        assert valid_emails == ["valid@b.com", "catchall@b.com"]

    def test_counters_track_status(self, base_cfg, tmp_path):
        base_cfg["output"]["output_dir"] = str(tmp_path)
        with ResultWriter(base_cfg) as w:
            w.write(_result("a@b.com", Status.VALID, SubStatus.CONFIRMED))
            w.write(_result("b@b.com", Status.VALID, SubStatus.CONFIRMED))
            w.write(_result("c@b.com", Status.INVALID, SubStatus.MAILBOX_NOT_FOUND))

        assert w.counters["valid"] == 2
        assert w.counters["invalid"] == 1

    def test_sub_counters_track_sub_status(self, base_cfg, tmp_path):
        base_cfg["output"]["output_dir"] = str(tmp_path)
        with ResultWriter(base_cfg) as w:
            w.write(_result("a@b.com", Status.UNKNOWN, SubStatus.GREYLISTED))
            w.write(_result("b@b.com", Status.UNKNOWN, SubStatus.GREYLISTED))
            w.write(_result("c@b.com", Status.UNKNOWN, SubStatus.RATE_LIMITED))

        assert w.sub_counters["greylisted"] == 2
        assert w.sub_counters["rate_limited"] == 1

    def test_append_mode_preserves_existing_content(self, base_cfg, tmp_path):
        base_cfg["output"]["output_dir"] = str(tmp_path)
        with ResultWriter(base_cfg, append=False) as w:
            w.write(_result("first@b.com", Status.VALID, SubStatus.CONFIRMED))

        with ResultWriter(base_cfg, append=True) as w:
            w.write(_result("second@b.com", Status.VALID, SubStatus.CONFIRMED))

        valid_emails = (tmp_path / "valid_emails.txt").read_text().splitlines()
        assert valid_emails == ["first@b.com", "second@b.com"]

    def test_overwrite_mode_clears_existing_content(self, base_cfg, tmp_path):
        base_cfg["output"]["output_dir"] = str(tmp_path)
        with ResultWriter(base_cfg, append=False) as w:
            w.write(_result("first@b.com", Status.VALID, SubStatus.CONFIRMED))

        with ResultWriter(base_cfg, append=False) as w:
            w.write(_result("second@b.com", Status.VALID, SubStatus.CONFIRMED))

        valid_emails = (tmp_path / "valid_emails.txt").read_text().splitlines()
        assert valid_emails == ["second@b.com"]

    def test_checkpoint_is_marked_on_write(self, base_cfg, tmp_path):
        base_cfg["output"]["output_dir"] = str(tmp_path)

        class FakeCheckpoint:
            def __init__(self):
                self.marked = []

            def mark(self, email):
                self.marked.append(email)

        cp = FakeCheckpoint()
        with ResultWriter(base_cfg, checkpoint=cp) as w:
            w.write(_result("a@b.com", Status.VALID, SubStatus.CONFIRMED))
            w.write(_result("b@b.com", Status.INVALID, SubStatus.MAILBOX_NOT_FOUND))

        assert cp.marked == ["a@b.com", "b@b.com"]

    def test_output_dir_created_if_missing(self, base_cfg, tmp_path):
        nested = tmp_path / "nested" / "dir"
        base_cfg["output"]["output_dir"] = str(nested)
        with ResultWriter(base_cfg) as w:
            w.write(_result("a@b.com", Status.VALID, SubStatus.CONFIRMED))
        assert nested.exists()


class TestSummariseJsonl:
    def test_counts_totals_by_status(self, tmp_path):
        p = tmp_path / "results.jsonl"
        rows = [
            _result("a@b.com", Status.VALID, SubStatus.CONFIRMED),
            _result("b@b.com", Status.VALID, SubStatus.CONFIRMED),
            _result("c@b.com", Status.INVALID, SubStatus.MAILBOX_NOT_FOUND),
        ]
        p.write_text("\n".join(r.to_jsonl() for r in rows) + "\n")

        s = summarise_jsonl(p)
        assert s["total"] == 3
        assert s["counts"]["valid"] == 2
        assert s["counts"]["invalid"] == 1

    def test_sub_counts_tracked(self, tmp_path):
        p = tmp_path / "results.jsonl"
        rows = [
            _result("a@b.com", Status.UNKNOWN, SubStatus.GREYLISTED),
            _result("b@b.com", Status.UNKNOWN, SubStatus.GREYLISTED),
        ]
        p.write_text("\n".join(r.to_jsonl() for r in rows) + "\n")

        s = summarise_jsonl(p)
        assert s["sub_counts"]["greylisted"] == 2

    def test_avg_duration_computed(self, tmp_path):
        p = tmp_path / "results.jsonl"
        rows = [
            _result("a@b.com", Status.VALID, SubStatus.CONFIRMED, duration_ms=100),
            _result("b@b.com", Status.VALID, SubStatus.CONFIRMED, duration_ms=200),
        ]
        p.write_text("\n".join(r.to_jsonl() for r in rows) + "\n")

        s = summarise_jsonl(p)
        assert s["avg_duration_ms"] == 150.0

    def test_catch_all_domains_collected(self, tmp_path):
        p = tmp_path / "results.jsonl"
        rows = [
            _result(
                "a@gmail.com", Status.CATCH_ALL, SubStatus.CATCH_ALL,
                is_catch_all_domain=True,
            ),
            _result(
                "b@gmail.com", Status.CATCH_ALL, SubStatus.CATCH_ALL,
                is_catch_all_domain=True,
            ),
            _result("c@normal.com", Status.VALID, SubStatus.CONFIRMED),
        ]
        p.write_text("\n".join(r.to_jsonl() for r in rows) + "\n")

        s = summarise_jsonl(p)
        assert s["catch_all_domains"] == ["gmail.com"]

    def test_skips_blank_and_corrupt_lines(self, tmp_path):
        p = tmp_path / "results.jsonl"
        good = _result("a@b.com", Status.VALID, SubStatus.CONFIRMED)
        p.write_text(good.to_jsonl() + "\n\nnot valid json{{{\n")

        s = summarise_jsonl(p)
        assert s["total"] == 1

    def test_empty_file_returns_zero_total(self, tmp_path):
        p = tmp_path / "results.jsonl"
        p.write_text("")
        s = summarise_jsonl(p)
        assert s["total"] == 0
        assert s["avg_duration_ms"] == 0

    def test_sample_errors_limited_to_ten(self, tmp_path):
        p = tmp_path / "results.jsonl"
        rows = [
            _result(f"user{i}@b.com", Status.UNKNOWN, SubStatus.CONNECT_FAILED, reason="timeout")
            for i in range(15)
        ]
        p.write_text("\n".join(r.to_jsonl() for r in rows) + "\n")

        s = summarise_jsonl(p)
        assert len(s["sample_errors"]) == 10