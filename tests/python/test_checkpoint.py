# Tests for checkpoint

from __future__ import annotations

import json
from pathlib import Path

import pytest

from smtp_stinger.checkpoint import CHECKPOINT_FILENAME, Checkpoint


class TestCheckpointMark:
    def test_mark_adds_to_completed(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=10)
        cp.mark("a@b.com")
        cp.mark("c@d.com")
        assert cp.completed == {"a@b.com", "c@d.com"}

    def test_mark_is_idempotent(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=10)
        cp.mark("a@b.com")
        cp.mark("a@b.com")
        assert cp.completed == {"a@b.com"}


class TestCheckpointSave:
    def test_save_creates_file(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=2)
        cp.mark("a@b.com")
        cp.save()
        assert cp.path.exists()
        assert cp.path.name == CHECKPOINT_FILENAME

    def test_save_writes_header_first_line(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=2)
        cp.mark("a@b.com")
        cp.save()

        lines = cp.path.read_text().splitlines()
        header = json.loads(lines[0])
        assert header["type"] == "header"
        assert header["total"] == 2
        assert header["completed"] == 1
        assert header["emails_file"] == "emails.txt"

    def test_save_writes_one_email_per_line(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=3)
        cp.mark("a@b.com")
        cp.mark("c@d.com")
        cp.save()

        lines = cp.path.read_text().splitlines()
        email_lines = [json.loads(l) for l in lines[1:]]
        emails = {e["email"] for e in email_lines}
        assert emails == {"a@b.com", "c@d.com"}
        assert all(e["type"] == "email" for e in email_lines)

    def test_save_does_not_leave_tmp_file(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=1)
        cp.mark("a@b.com")
        cp.save()
        tmp_file = cp.path.with_suffix(".tmp")
        assert not tmp_file.exists()

    def test_save_overwrites_previous_save(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=2)
        cp.mark("a@b.com")
        cp.save()
        cp.mark("c@d.com")
        cp.save()

        loaded = Checkpoint.load(cp.path)
        assert loaded == {"a@b.com", "c@d.com"}


class TestCheckpointDelete:
    def test_delete_removes_file(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=1)
        cp.mark("a@b.com")
        cp.save()
        assert cp.path.exists()
        cp.delete()
        assert not cp.path.exists()

    def test_delete_on_nonexistent_file_does_not_raise(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=1)
        cp.delete()  # never saved — should be a no-op


class TestCheckpointLoad:
    def test_load_missing_file_raises_value_error(self, tmp_output_dir: Path):
        with pytest.raises(ValueError, match="not found"):
            Checkpoint.load(tmp_output_dir / "nonexistent.jsonl")

    def test_load_returns_completed_emails(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=3)
        cp.mark("a@b.com")
        cp.mark("c@d.com")
        cp.save()

        loaded = Checkpoint.load(cp.path)
        assert loaded == {"a@b.com", "c@d.com"}

    def test_load_skips_header_line(self, tmp_output_dir: Path):
        p = tmp_output_dir / "checkpoint.jsonl"
        p.write_text(
            json.dumps({"type": "header", "total": 1}) + "\n"
            + json.dumps({"type": "email", "email": "a@b.com"}) + "\n"
        )
        loaded = Checkpoint.load(p)
        assert loaded == {"a@b.com"}

    def test_load_skips_blank_lines(self, tmp_output_dir: Path):
        p = tmp_output_dir / "checkpoint.jsonl"
        p.write_text(
            json.dumps({"type": "header", "total": 1}) + "\n\n"
            + json.dumps({"type": "email", "email": "a@b.com"}) + "\n\n"
        )
        loaded = Checkpoint.load(p)
        assert loaded == {"a@b.com"}

    def test_load_corrupt_json_raises_value_error(self, tmp_output_dir: Path):
        p = tmp_output_dir / "checkpoint.jsonl"
        p.write_text('{"type": "header"}\nnot valid json{{{\n')
        with pytest.raises(ValueError, match="corrupt"):
            Checkpoint.load(p)

    def test_load_ignores_email_lines_with_empty_email(self, tmp_output_dir: Path):
        p = tmp_output_dir / "checkpoint.jsonl"
        p.write_text(
            json.dumps({"type": "header", "total": 1}) + "\n"
            + json.dumps({"type": "email", "email": ""}) + "\n"
        )
        loaded = Checkpoint.load(p)
        assert loaded == set()


class TestCheckpointFind:
    def test_find_returns_none_when_absent(self, tmp_output_dir: Path):
        assert Checkpoint.find(tmp_output_dir) is None

    def test_find_returns_path_when_present(self, tmp_output_dir: Path):
        cp = Checkpoint(tmp_output_dir, "emails.txt", total=1)
        cp.mark("a@b.com")
        cp.save()

        found = Checkpoint.find(tmp_output_dir)
        assert found == cp.path


class TestCheckpointRoundTrip:
    def test_full_save_load_resume_cycle(self, tmp_output_dir: Path):
        """Simulates: run partially, save, resume from checkpoint."""
        all_emails = ["a@b.com", "c@d.com", "e@f.com", "g@h.com"]

        cp = Checkpoint(tmp_output_dir, "emails.txt", total=len(all_emails))
        for email in all_emails[:2]:
            cp.mark(email)
        cp.save()

        # Simulate a fresh process loading the checkpoint to resume
        completed = Checkpoint.load(cp.path)
        remaining = [e for e in all_emails if e not in completed]

        assert remaining == ["e@f.com", "g@h.com"]