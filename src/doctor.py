# Validates DNS setup for helo_hostname and mail_from in config


from __future__ import annotations

import socket
from typing import Optional

import dns.resolver
import dns.reversename


def _get_my_ip() -> Optional[str]:
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("8.8.8.8", 80))
        ip = s.getsockname()[0]
        s.close()
        return ip
    except Exception:
        return None


def _resolve_a(hostname: str) -> Optional[str]:
    try:
        answers = dns.resolver.resolve(hostname, "A")
        return str(answers[0])
    except Exception:
        return None


def _resolve_ptr(ip: str) -> Optional[str]:
    try:
        rev = dns.reversename.from_address(ip)
        answers = dns.resolver.resolve(rev, "PTR")
        return str(answers[0]).rstrip(".")
    except Exception:
        return None


def _resolve_spf(domain: str) -> Optional[str]:
    try:
        answers = dns.resolver.resolve(domain, "TXT")
        for rdata in answers:
            txt = "".join(s.decode() for s in rdata.strings)
            if txt.startswith("v=spf1"):
                return txt
        return None
    except Exception:
        return None


def _resolve_mx(domain: str) -> list[str]:
    try:
        answers = dns.resolver.resolve(domain, "MX")
        return [str(r.exchange).rstrip(".") for r in sorted(answers, key=lambda r: r.preference)]
    except Exception:
        return []


def run_doctor(helo_hostname: str, mail_from: str) -> bool:
    # Run all, return report
    mail_from_domain = mail_from.split("@")[1] if "@" in mail_from else mail_from
    my_ip = _get_my_ip()

    ok = True
    checks_passed = 0
    checks_total = 0

    def check(label: str, passed: bool, detail: str, critical: bool = True) -> None:
        nonlocal ok, checks_passed, checks_total
        checks_total += 1
        icon = "✓" if passed else ("✗" if critical else "⚠")
        status = "OK" if passed else ("FAIL" if critical else "WARN")
        color = "\033[32m" if passed else ("\033[31m" if critical else "\033[33m")
        reset = "\033[0m"
        print(f"  {color}{icon}{reset}  [{status:4s}]  {label}")
        print(f"          {detail}")
        if passed:
            checks_passed += 1
        elif critical:
            ok = False

    print()
    print("  ╔══════════════════════════════════════════════════╗")
    print("  ║           SMTP-Stinger — DNS Doctor              ║")
    print("  ╚══════════════════════════════════════════════════╝")
    print()
    print(f"  helo_hostname : {helo_hostname}")
    print(f"  mail_from     : {mail_from}")
    print(f"  this machine  : {my_ip or 'unknown'}")
    print()
    print("  ── A Record ──────────────────────────────────────")

    a_ip = _resolve_a(helo_hostname)
    if a_ip:
        check(
            f"A record for {helo_hostname}",
            True,
            f"{helo_hostname} → {a_ip}",
        )
    else:
        check(
            f"A record for {helo_hostname}",
            False,
            f"No A record found. Add:  {helo_hostname}  A  <YOUR_IP>",
        )

    print()
    print("  ── PTR / Reverse DNS ─────────────────────────────")

    if a_ip:
        ptr = _resolve_ptr(a_ip)
        if ptr and ptr.lower() == helo_hostname.lower():
            check(
                f"PTR matches helo_hostname",
                True,
                f"{a_ip} → {ptr}",
            )
        elif ptr:
            check(
                "PTR matches helo_hostname",
                False,
                f"{a_ip} → {ptr}  (expected {helo_hostname})\n"
                f"          Fix in your VPS/host panel under rDNS or Reverse DNS.",
            )
        else:
            check(
                "PTR record exists",
                False,
                f"No PTR record for {a_ip}.\n"
                f"          Set rDNS/Reverse DNS at your VPS/host panel to: {helo_hostname}",
            )
    else:
        check("PTR record", False, "Skipped — A record missing.", critical=False)

    if my_ip and a_ip and my_ip != a_ip:
        check(
            "A record matches this machine's IP",
            False,
            f"A record points to {a_ip} but this machine's IP is {my_ip}.\n"
            f"          Make sure you're running SMTP-Stinger from the right server.",
            critical=False,
        )
    elif my_ip and a_ip:
        check(
            "A record matches this machine's IP",
            True,
            f"Both resolve to {a_ip}",
        )

    print()
    print("  ── SPF Record ────────────────────────────────────")

    spf = _resolve_spf(mail_from_domain)
    if spf:
        has_ip = my_ip and (f"ip4:{my_ip}" in spf or "ip4:0.0.0.0/0" in spf or "+all" in spf or "~all" in spf)
        check(
            f"SPF record for {mail_from_domain}",
            True,
            f"{spf}",
        )
        if my_ip and f"ip4:{my_ip}" not in spf and "+all" not in spf:
            check(
                "SPF includes this machine's IP",
                False,
                f"Your IP {my_ip} is not explicitly in the SPF record.\n"
                f"          Suggested record:  v=spf1 ip4:{my_ip} ~all",
                critical=False,
            )
        else:
            check(
                "SPF includes this machine's IP",
                True,
                f"ip4:{my_ip} found or permissive policy present",
            )
    else:
        check(
            f"SPF record for {mail_from_domain}",
            False,
            f"No SPF record found.\n"
            f"          Add TXT record:  {mail_from_domain}  \"v=spf1 ip4:{my_ip or 'YOUR_IP'} ~all\"",
            critical=False,  # Warn but don't block — some servers don't require it
        )

    print()
    print("  ── mail_from domain MX (optional) ────────────────")

    mxs = _resolve_mx(mail_from_domain)
    if mxs:
        check(
            f"MX records for {mail_from_domain}",
            True,
            f"Found: {', '.join(mxs[:3])}",
        )
    else:
        check(
            f"MX records for {mail_from_domain}",
            True,  # Not required — mail_from domain doesn't need to receive
            f"None found — that's fine, the sender domain doesn't need to receive mail.",
        )

    print()
    print(f"  ── Summary {'─' * 38}")
    color = "\033[32m" if ok else "\033[31m"
    reset = "\033[0m"
    print(f"  {color}{'All checks passed' if ok else 'Some checks failed — see above'}{reset}")
    print(f"  {checks_passed}/{checks_total} checks passed")
    print()

    if not ok:
        print("  Once fixed, re-run:  stinger doctor")
        print()

    return ok