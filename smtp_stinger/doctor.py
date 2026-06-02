# Validates DNS setup for helo_hostname and mail_from in config
# Uses the same DNS layer as the SMTP pipeline (configure=False + explicit resolvers)

from __future__ import annotations

import asyncio
import ipaddress
import socket
from typing import Optional

import dns.asyncresolver
import dns.exception
import dns.resolver
import dns.reversename


def build_resolver(cfg: dict) -> dns.asyncresolver.Resolver:
    # Build a resolver the same way the SMTP pipeline does.
    # configure=False avoids relying on /etc/resolv.conf (broken on macOS in some envs).
    # Falls back to well-known public resolvers if none are configured.

    resolver = dns.asyncresolver.Resolver(configure=False)
    resolvers = cfg.get("dns", {}).get("resolvers") or []
    if resolvers:
        resolver.nameservers = resolvers
    else:
        # Sensible fallback — don't leave nameservers empty
        resolver.nameservers = ["1.1.1.1", "8.8.8.8"]
    return resolver


# DNS lookups (async, explicit error types)
async def resolve_a(resolver: dns.asyncresolver.Resolver, hostname: str) -> tuple[Optional[str], Optional[str]]:
    try:
        answers = await resolver.resolve(hostname, "A")
        return str(answers[0]), None
    except dns.resolver.NXDOMAIN:
        return None, "domain does not exist (NXDOMAIN)"
    except dns.resolver.NoAnswer:
        return None, "no A record present"
    except dns.resolver.NoNameservers:
        return None, "DNS server failed to respond (NoNameservers)"
    except dns.exception.Timeout:
        return None, "DNS query timed out"
    except Exception as e:
        return None, f"{type(e).__name__}: {e}"


async def resolve_ptr(resolver: dns.asyncresolver.Resolver, ip: str) -> tuple[Optional[str], Optional[str]]:
    try:
        rev = dns.reversename.from_address(ip)
        answers = await resolver.resolve(rev, "PTR")
        return str(answers[0]).rstrip("."), None
    except dns.resolver.NXDOMAIN:
        return None, "no PTR record (NXDOMAIN)"
    except dns.resolver.NoAnswer:
        return None, "no PTR record present"
    except dns.resolver.NoNameservers:
        return None, "DNS server failed to respond"
    except dns.exception.Timeout:
        return None, "DNS query timed out"
    except Exception as e:
        return None, f"{type(e).__name__}: {e}"


async def resolve_spf(resolver: dns.asyncresolver.Resolver, domain: str) -> tuple[Optional[str], Optional[str]]:
    try:
        answers = await resolver.resolve(domain, "TXT")
        for rdata in answers:
            txt = "".join(
                s.decode() if isinstance(s, bytes) else str(s)
                for s in rdata.strings
            )
            if txt.startswith("v=spf1"):
                return txt, None
        return None, None  # No error, just no SPF record
    except dns.resolver.NXDOMAIN:
        return None, "domain does not exist (NXDOMAIN)"
    except dns.resolver.NoAnswer:
        return None, None  # TXT exists but no SPF
    except dns.resolver.NoNameservers:
        return None, "DNS server failed to respond"
    except dns.exception.Timeout:
        return None, "DNS query timed out"
    except Exception as e:
        return None, f"{type(e).__name__}: {e}"


async def resolve_mx(resolver: dns.asyncresolver.Resolver, domain: str) -> list[str]:
    """Returns sorted MX hostnames, empty list on any failure."""
    try:
        answers = await resolver.resolve(domain, "MX")
        return [
            str(r.exchange).rstrip(".")
            for r in sorted(answers, key=lambda r: r.preference)
        ]
    except (dns.resolver.NXDOMAIN, dns.resolver.NoAnswer,
            dns.resolver.NoNameservers, dns.exception.Timeout, Exception):
        return []



def _get_local_ip() -> Optional[str]:
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("8.8.8.8", 80))
        ip = s.getsockname()[0]
        s.close()
        return ip
    except Exception:
        return None


def _is_private(ip: str) -> bool:
    try:
        return ipaddress.ip_address(ip).is_private
    except ValueError:
        return False



def run_doctor(helo_hostname: str, mail_from: str, cfg: dict | None = None) -> bool:
    return asyncio.run(_run_doctor_async(helo_hostname, mail_from, cfg or {}))


async def _run_doctor_async(helo_hostname: str, mail_from: str, cfg: dict) -> bool:
    resolver = build_resolver(cfg)
    mail_from_domain = mail_from.split("@")[1] if "@" in mail_from else mail_from

    # Local IP — only used for A record cross-check, never for SPF suggestions
    local_ip = _get_local_ip()
    local_is_private = local_ip is not None and _is_private(local_ip)

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
    ns = resolver.nameservers
    print(f"  resolvers     : {', '.join(ns)}")
    if local_ip and not local_is_private:
        print(f"  this machine  : {local_ip}")
    elif local_ip and local_is_private:
        print(f"  this machine  : {local_ip} (private — not shown in suggestions)")
    print()

    print("  ── A Record ──────────────────────────────────────")

    a_ip, a_err = await resolve_a(resolver, helo_hostname)
    if a_ip:
        check(f"A record for {helo_hostname}", True, f"{helo_hostname} → {a_ip}")
    else:
        check(
            f"A record for {helo_hostname}",
            False,
            f"Lookup failed: {a_err}\n"
            f"          Add:  {helo_hostname}  A  <YOUR_SERVER_IP>",
        )

    print()
    print("  ── PTR / Reverse DNS ─────────────────────────────")

    if a_ip:
        ptr, ptr_err = await resolve_ptr(resolver, a_ip)
        if ptr and ptr.lower() == helo_hostname.lower():
            check("PTR matches helo_hostname", True, f"{a_ip} → {ptr}")
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
                f"Lookup failed: {ptr_err}\n"
                f"          Set rDNS/Reverse DNS at your VPS/host panel to: {helo_hostname}",
            )

        if local_ip and not local_is_private and a_ip != local_ip:
            check(
                "A record matches this machine's IP",
                False,
                f"A record → {a_ip}, this machine → {local_ip}\n"
                f"          Make sure you're running stinger from the correct server.",
                critical=False,
            )
        elif local_ip and not local_is_private:
            check("A record matches this machine's IP", True, f"Both resolve to {a_ip}")
    else:
        check("PTR record", False, "Skipped — A record lookup failed.", critical=False)

    print()
    print("  ── SPF Record ────────────────────────────────────")

    spf, spf_err = await resolve_spf(resolver, mail_from_domain)

    if spf_err:
        check(
            f"SPF record for {mail_from_domain}",
            False,
            f"Lookup failed: {spf_err}",
            critical=False,
        )
    elif spf:
        check(f"SPF record for {mail_from_domain}", True, spf)

        # Check if the A record IP is covered — only meaningful if we have it
        if a_ip and f"ip4:{a_ip}" not in spf and "+all" not in spf:
            check(
                "SPF covers server IP",
                False,
                f"ip4:{a_ip} not found in SPF record.\n"
                f"          Suggested record:  v=spf1 ip4:{a_ip} ~all",
                critical=False,
            )
        elif a_ip:
            check("SPF covers server IP", True, f"ip4:{a_ip} found or permissive policy present")
    else:
        # Build a useful suggestion — use A record IP if available, never a private IP
        if a_ip:
            suggest_spf = f'v=spf1 ip4:{a_ip} ~all'
        else:
            suggest_spf = 'v=spf1 ip4:<YOUR_SERVER_IP> ~all'

        check(
            f"SPF record for {mail_from_domain}",
            False,
            f"No SPF TXT record found.\n"
            f"          Add TXT record:  {mail_from_domain}  \"{suggest_spf}\"",
            critical=False,
        )

    print()
    print("  ── mail_from domain MX (optional) ────────────────")

    mxs = await resolve_mx(resolver, mail_from_domain)
    if mxs:
        check(f"MX records for {mail_from_domain}", True, f"Found: {', '.join(mxs[:3])}")
    else:
        check(
            f"MX records for {mail_from_domain}",
            True,
            "None found — that's fine, the sender domain doesn't need to receive mail.",
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