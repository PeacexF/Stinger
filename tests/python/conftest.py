# Shared fixtures for the smtp_stinger test suite

from __future__ import annotations

import sys
from pathlib import Path

import pytest

# Make smtp_stinger importable regardless of where pytest is invoked from
ROOT = Path(__file__).resolve().parents[2]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


@pytest.fixture
def base_cfg() -> dict:
    """A minimal, valid config dict — mirrors config.py defaults."""
    return {
        "smtp": {
            "helo_hostname": "mail.example.com",
            "mail_from": "verify@example.com",
            "connect_timeout_sec": 10,
            "command_timeout_sec": 15,
            "port": 25,
            "try_tls": True,
        },
        "concurrency": {
            "global_limit": 100,
            "per_domain_limit": 2,
        },
        "dns": {
            "mx_cache_ttl": 3600,
            "catch_all_cache_ttl": 3600,
            "resolvers": ["1.1.1.1", "8.8.8.8"],
        },
        "retry": {
            "max_attempts": 3,
            "backoff_base_sec": 2,
        },
        "output": {
            "output_dir": "./results",
            "valid_txt": "valid_emails.txt",
            "full_jsonl": "results.jsonl",
        },
        "input": {
            "emails_file": "./emails.txt",
        },
        "logging": {
            "level": "INFO",
            "show_progress": True,
        },
    }


@pytest.fixture
def tmp_output_dir(tmp_path: Path) -> Path:
    d = tmp_path / "results"
    d.mkdir()
    return d