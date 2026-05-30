# Config loading, validation, and default template 


from __future__ import annotations

import sys
from pathlib import Path
from typing import Any

import yaml

DEFAULT_CONFIG_TEMPLATE = """\
# ─────────────────────────────────────────────────────────────
#  SMTP-Stinger  —  config.yaml
#  Fill in the smtp section before running.
#  Run  stinger doctor  to validate your DNS setup.
# ─────────────────────────────────────────────────────────────

smtp:
  # Your sending identity. Must be a real domain you control.
  # Requirements:
  #   A record   : mail.yourdomain.com → YOUR.SERVER.IP
  #   PTR record : YOUR.SERVER.IP → mail.yourdomain.com  (set at your host/VPS panel)
  #   SPF record : yourdomain.com TXT "v=spf1 ip4:YOUR.SERVER.IP ~all"
  helo_hostname: ""         # e.g. "mail.yourdomain.com"
  mail_from: ""             # e.g. "stinger@yourdomain.com"

  connect_timeout_sec: 10
  command_timeout_sec: 15
  port: 25
  try_tls: true

concurrency:
  global_limit: 100         # max simultaneous SMTP connections
  per_domain_limit: 2       # max connections to any single domain

dns:
  mx_cache_ttl: 3600
  catch_all_cache_ttl: 3600
  resolvers: []
  # - "8.8.8.8"
  # - "1.1.1.1"

retry:
  max_attempts: 3
  backoff_base_sec: 2

output:
  output_dir: "./results"
  valid_txt: "valid_emails.txt"
  full_jsonl: "results.jsonl"

input:
  emails_file: "./emails.txt"

logging:
  level: "INFO"
  show_progress: true
"""


def scaffold_config(dest: Path) -> None:
    dest.write_text(DEFAULT_CONFIG_TEMPLATE)


def load_config(path: Path | str, require_smtp: bool = True) -> dict[str, Any]:
    # Load and minimally validate config.yaml.
    # If require_smtp=False, skips the helo/mail_from check (used by `init` and `doctor --check-dns`).

    path = Path(path)
    if not path.exists():
        _die(
            f"Config file not found: {path}\n"
            "  Run  stinger init  to create one."
        )

    with open(path) as f:
        cfg = yaml.safe_load(f)

    if cfg is None:
        _die(f"Config file is empty: {path}")

    # Apply defaults for optional keys
    cfg.setdefault("concurrency", {})
    cfg["concurrency"].setdefault("global_limit", 100)
    cfg["concurrency"].setdefault("per_domain_limit", 2)

    cfg.setdefault("dns", {})
    cfg["dns"].setdefault("mx_cache_ttl", 3600)
    cfg["dns"].setdefault("catch_all_cache_ttl", 3600)
    cfg["dns"].setdefault("resolvers", [])

    cfg.setdefault("retry", {})
    cfg["retry"].setdefault("max_attempts", 3)
    cfg["retry"].setdefault("backoff_base_sec", 2)

    cfg.setdefault("output", {})
    cfg["output"].setdefault("output_dir", "./results")
    cfg["output"].setdefault("valid_txt", "valid_emails.txt")
    cfg["output"].setdefault("full_jsonl", "results.jsonl")

    cfg.setdefault("input", {})
    cfg["input"].setdefault("emails_file", "./emails.txt")

    cfg.setdefault("logging", {})
    cfg["logging"].setdefault("level", "INFO")
    cfg["logging"].setdefault("show_progress", True)

    cfg["smtp"].setdefault("connect_timeout_sec", 10)
    cfg["smtp"].setdefault("command_timeout_sec", 15)
    cfg["smtp"].setdefault("port", 25)
    cfg["smtp"].setdefault("try_tls", True)

    if require_smtp:
        helo = cfg["smtp"].get("helo_hostname", "").strip()
        mail_from = cfg["smtp"].get("mail_from", "").strip()
        if not helo or not mail_from:
            _die(
                "smtp.helo_hostname and smtp.mail_from must be set in config.yaml.\n"
                "  Use a real domain with matching A, PTR, and SPF records.\n"
                "  Run  stinger doctor  to validate your DNS setup."
            )

    return cfg


def _die(msg: str) -> None:
    print(f"\n[stinger] ERROR: {msg}\n", file=sys.stderr)
    sys.exit(1)