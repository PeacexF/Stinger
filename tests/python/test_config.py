# Tests for config

from __future__ import annotations

from pathlib import Path

import pytest
import yaml

from smtp_stinger.config import load_config, scaffold_config


class TestScaffoldConfig:
    def test_creates_file(self, tmp_path: Path):
        dest = tmp_path / "config.yaml"
        scaffold_config(dest)
        assert dest.exists()

    def test_scaffolded_file_is_valid_yaml(self, tmp_path: Path):
        dest = tmp_path / "config.yaml"
        scaffold_config(dest)
        data = yaml.safe_load(dest.read_text())
        assert "smtp" in data
        assert "concurrency" in data

    def test_scaffolded_helo_and_mail_from_are_blank(self, tmp_path: Path):
        dest = tmp_path / "config.yaml"
        scaffold_config(dest)
        data = yaml.safe_load(dest.read_text())
        assert data["smtp"]["helo_hostname"] == ""
        assert data["smtp"]["mail_from"] == ""


class TestLoadConfig:
    def _write(self, tmp_path: Path, content: dict) -> Path:
        p = tmp_path / "config.yaml"
        p.write_text(yaml.dump(content))
        return p

    def test_missing_file_exits(self, tmp_path: Path):
        with pytest.raises(SystemExit):
            load_config(tmp_path / "does_not_exist.yaml")

    def test_empty_file_exits(self, tmp_path: Path):
        p = tmp_path / "config.yaml"
        p.write_text("")
        with pytest.raises(SystemExit):
            load_config(p)

    def test_missing_helo_exits_when_required(self, tmp_path: Path):
        p = self._write(tmp_path, {"smtp": {"helo_hostname": "", "mail_from": "a@b.com"}})
        with pytest.raises(SystemExit):
            load_config(p, require_smtp=True)

    def test_missing_mail_from_exits_when_required(self, tmp_path: Path):
        p = self._write(tmp_path, {"smtp": {"helo_hostname": "mail.b.com", "mail_from": ""}})
        with pytest.raises(SystemExit):
            load_config(p, require_smtp=True)

    def test_valid_smtp_does_not_exit(self, tmp_path: Path):
        p = self._write(
            tmp_path,
            {"smtp": {"helo_hostname": "mail.b.com", "mail_from": "a@b.com"}},
        )
        cfg = load_config(p, require_smtp=True)
        assert cfg["smtp"]["helo_hostname"] == "mail.b.com"

    def test_require_smtp_false_skips_validation(self, tmp_path: Path):
        p = self._write(tmp_path, {"smtp": {"helo_hostname": "", "mail_from": ""}})
        cfg = load_config(p, require_smtp=False)
        assert cfg["smtp"]["helo_hostname"] == ""

    def test_defaults_applied_for_missing_sections(self, tmp_path: Path):
        p = self._write(
            tmp_path,
            {"smtp": {"helo_hostname": "mail.b.com", "mail_from": "a@b.com"}},
        )
        cfg = load_config(p, require_smtp=True)
        assert cfg["concurrency"]["global_limit"] == 100
        assert cfg["concurrency"]["per_domain_limit"] == 2
        assert cfg["dns"]["mx_cache_ttl"] == 3600
        assert cfg["retry"]["max_attempts"] == 3
        assert cfg["output"]["output_dir"] == "./results"
        assert cfg["logging"]["level"] == "INFO"

    def test_explicit_values_are_not_overridden_by_defaults(self, tmp_path: Path):
        p = self._write(
            tmp_path,
            {
                "smtp": {"helo_hostname": "mail.b.com", "mail_from": "a@b.com"},
                "concurrency": {"global_limit": 50, "per_domain_limit": 1},
            },
        )
        cfg = load_config(p, require_smtp=True)
        assert cfg["concurrency"]["global_limit"] == 50
        assert cfg["concurrency"]["per_domain_limit"] == 1

    def test_smtp_optional_fields_get_defaults(self, tmp_path: Path):
        p = self._write(
            tmp_path,
            {"smtp": {"helo_hostname": "mail.b.com", "mail_from": "a@b.com"}},
        )
        cfg = load_config(p, require_smtp=True)
        assert cfg["smtp"]["connect_timeout_sec"] == 10
        assert cfg["smtp"]["port"] == 25
        assert cfg["smtp"]["try_tls"] is True

    def test_whitespace_only_helo_is_rejected(self, tmp_path: Path):
        p = self._write(
            tmp_path,
            {"smtp": {"helo_hostname": "   ", "mail_from": "a@b.com"}},
        )
        with pytest.raises(SystemExit):
            load_config(p, require_smtp=True)