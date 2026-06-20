# DNS Setup Guide: A, SPF, DKIM, and DMARC

## Why this matters for SMTP-Stinger

When Stinger connects to a mail server to verify an address, it identifies itself via `HELO`/`EHLO` and `MAIL FROM`. Modern mail servers cross-check this identity against DNS before responding. A domain with no A record, no matching PTR, no SPF, and no DKIM looks identical to a spam source - so you'll see far more `550`, `554`, and silent timeouts than necessary. None of these records are optional if you want consistent results.

---

## 1. A Record

The A record points your sending hostname to your server's IPv4 address. It is the most basic identity check - many servers won't even accept your `HELO` without it resolving.

### What to set

| Type | Host | Value |
|---|---|---|
| A | `mail.yourdomain.com` | `128.1.1.0` (your server's public IP) |

### How

In your **DNS provider's** dashboard (Cloudflare, Route53, Namecheap, etc...):

1. Add a new **A record**
2. Host/Name: `mail` (or whatever subdomain you'll use as your `helo_hostname`)
3. Value: your server's public IPv4 address
4. TTL: 3600 (1 hour) is fine - lower it temporarily while testing if you need faster propagation

### Verify

```bash
dig A mail.yourdomain.com +short
```

Should print your server's IP. If it returns nothing, wait for propagation (a few minutes to a few hours depending on your provider and TTL).

---

## 2. PTR Record (Reverse DNS)

The PTR record is the reverse of the A record - it maps your IP back to a hostname. This is set at your **hosting provider**, not your DNS registrar, because only the IP's owner (your VPS/cloud provider) controls reverse DNS for that IP.

A and PTR together form **FCrDNS** (Forward-Confirmed reverse DNS) - when `mail.yourdomain.com → IP` and `IP → mail.yourdomain.com` both resolve and match, you've passed one of the most common spam heuristics.

### How

Locate this setting in your provider's control panel - it's usually called "Reverse DNS," "rDNS," or "PTR Record":

| Provider | Location |
|---|---|
| Hetzner | Cloud Console → your server → Networking → tab "Reverse DNS" |
| DigitalOcean | Networking → Domains, or via `doctl` / support ticket |
| OVH | Manager → IP → your IP → "Reverse DNS" |
| AWS EC2 | Requires a support ticket (Elastic IP reverse DNS request) |
| Vultr | Server → Settings → IPv4 → click "edit" next to reverse DNS |
| Linode | Network tab → click your IP → "Edit RDNS" |

Set it to exactly `mail.yourdomain.com` (same hostname as your A record).

### Verify

```bash
dig -x 128.1.1.0 +short
```

Should print `mail.yourdomain.com.` (note the trailing dot - that's normal). It must match your A record hostname exactly.

---

## 3. SPF (Sender Policy Framework)

SPF is a TXT record listing which IPs/servers are authorized to send mail claiming to be from your domain. Receiving servers check the connecting IP against your domain's SPF record. Without it, mail (and verification probes) from your server look spoofable.

### What to set

| Type | Host | Value |
|---|---|---|
| TXT | `yourdomain.com` (root, not a subdomain) | `v=spf1 ip4:128.1.1.0 ~all` |

### Understanding the syntax

```
v=spf1 ip4:128.1.1.0 ~all
```

- `v=spf1` - declares this is an SPF record, must be first
- `ip4:128.1.1.0` - authorizes this specific IPv4 address. Add more with additional `ip4:` entries, or use a CIDR range like `ip4:128.1.1.0/24`
- `~all` - **soft fail**. Anything not listed above is marked suspicious but not auto-rejected. This is the right choice while testing or for a low-volume verification domain
- `-all` - **hard fail**. Stricter, used by established sending domains. Switch to this only once you're confident your SPF list is complete - getting it wrong with `-all` can cause legitimate mail to bounce

If you're sending from multiple IPs or also use a service like Google Workspace on the same domain, combine them:

```
v=spf1 ip4:128.1.1.0 include:_spf.google.com ~all
```

### Common mistakes

- **Only one SPF TXT record per domain.** Multiple `v=spf1` records cause a "PermError" and the record is treated as invalid by receivers. If you already have an SPF record (e.g. from Google Workspace), merge into it - don't add a second one.
- **Don't set it on a subdomain unless you're sending from that subdomain.** SPF applies to whatever domain appears in the `MAIL FROM` address - if `mail_from: stinger@yourdomain.com`, the SPF record goes on `yourdomain.com`, not `mail.yourdomain.com`.

### Verify

```bash
dig TXT yourdomain.com +short
```

Should include a line starting with `v=spf1`. You can also check syntax validity at [mxtoolbox.com/spf.aspx](https://mxtoolbox.com) or similar SPF checker tools.

---

## 4. DKIM (DomainKeys Identified Mail)

DKIM adds a cryptographic signature to outgoing mail, letting receivers verify the message wasn't altered in transit and genuinely originated from your domain. Unlike SPF (which checks the connecting IP) and the A/PTR pair (which check the hostname), DKIM operates at the message level - it's less critical for a pure SMTP-probing tool like Stinger (which sends `RCPT TO` but never delivers a full message body), but it matters if your domain *also* sends real mail, and an increasing number of receiving servers factor in whether DKIM is configured at all when scoring sender reputation.

### How DKIM works

1. You generate a public/private keypair
2. The private key signs outgoing mail headers
3. The public key is published in DNS
4. Receivers fetch the public key and verify the signature

### Generating a keypair

Using OpenSSL:

```bash
openssl genrsa -out dkim_private.pem 2048
openssl rsa -in dkim_private.pem -pubout -out dkim_public.pem
```

Extract just the key data (no headers/footers) for the DNS record:

```bash
grep -v -- '-----' dkim_public.pem | tr -d '\n'
```

This gives you a long base64 string - that's the value you'll publish.

### What to set

| Type | Host | Value |
|---|---|---|
| TXT | `selector._domainkey.yourdomain.com` | `v=DKIM1; k=rsa; p=<your-public-key-base64>` |

`selector` is an arbitrary name you choose to identify this key (lets you rotate keys later without breaking old signatures). Common choices: `mail`, `default`, `stinger`, or a date like `2026a`.

Example:

| Type | Host | Value |
|---|---|---|
| TXT | `stinger._domainkey.yourdomain.com` | `v=DKIM1; k=rsa; p=MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQC7...` |

### Configuring your mail software to sign with it

If you're using Postfix with OpenDKIM, or a similar setup, you'd reference the private key and selector in your DKIM signer config (e.g. `opendkim.conf`):

```
Selector            stinger
KeyFile             /etc/opendkim/keys/yourdomain.com/stinger.private
Domain              yourdomain.com
```

If Stinger itself is only probing (not delivering full signed messages), DKIM signing isn't strictly required for the verifier to function - but publishing the record still helps your domain's overall reputation if you send any real mail from the same IP.

### Verify

```bash
dig TXT stinger._domainkey.yourdomain.com +short
```

Should print your `v=DKIM1; k=rsa; p=...` record.

---

## 5. DMARC (Domain-based Message Authentication, Reporting & Conformance)

DMARC ties SPF and DKIM together and tells receiving servers what to do when a message fails either check, plus gives you visibility via aggregate reports. It's published as a single TXT record and is the easiest of the four to set up.

### What to set

| Type | Host | Value |
|---|---|---|
| TXT | `_dmarc.yourdomain.com` | `v=DMARC1; p=none; rua=mailto:dmarc-reports@yourdomain.com` |

### Understanding the syntax

- `v=DMARC1` - version, must be first
- `p=none` - policy for the domain itself. Options:
  - `none` - take no action, just report (recommended starting point)
  - `quarantine` - send failing mail to spam/junk
  - `reject` - refuse failing mail outright
- `rua=mailto:...` - where to send daily aggregate reports (optional but recommended - these reports show you who's sending mail claiming to be from your domain, including any misconfigurations on your own end)
- `pct=100` - (optional, default 100) percentage of failing mail the policy applies to; useful for gradually ramping up enforcement
- `sp=` - (optional) separate policy for subdomains

### Recommended rollout

Start permissive and tighten over time, since jumping straight to `reject` can silently break legitimate mail if your SPF/DKIM aren't perfectly configured yet:

```
# Week 1 - observe only
v=DMARC1; p=none; rua=mailto:dmarc-reports@yourdomain.com

# Week 3+ - once reports look clean, quarantine failures
v=DMARC1; p=quarantine; pct=100; rua=mailto:dmarc-reports@yourdomain.com

# Month 2+ - full enforcement once confident
v=DMARC1; p=reject; pct=100; rua=mailto:dmarc-reports@yourdomain.com
```

### Verify

```bash
dig TXT _dmarc.yourdomain.com +short
```

Should print your `v=DMARC1; ...` record.

---

## Full example: putting it all together

For a domain `yourdomain.com` with server IP `128.1.1.0`, sending from `stinger@yourdomain.com` via hostname `mail.yourdomain.com`:

| Record | Host | Value |
|---|---|---|
| A | `mail.yourdomain.com` | `128.1.1.0` |
| PTR | *(set at hosting provider for 128.1.1.0)* | `mail.yourdomain.com` |
| TXT (SPF) | `yourdomain.com` | `v=spf1 ip4:128.1.1.0 ~all` |
| TXT (DKIM) | `stinger._domainkey.yourdomain.com` | `v=DKIM1; k=rsa; p=<public-key>` |
| TXT (DMARC) | `_dmarc.yourdomain.com` | `v=DMARC1; p=none; rua=mailto:dmarc-reports@yourdomain.com` |

Matching `config.yaml`:

```yaml
smtp:
  helo_hostname: "mail.yourdomain.com"
  mail_from: "stinger@yourdomain.com"
```

---

## Verifying everything at once

SMTP-Stinger's built-in diagnostic checks A, PTR, and SPF automatically:

```bash
stinger doctor
```

For a full external check including DKIM and DMARC, these free tools are useful:

- [mxtoolbox.com](https://mxtoolbox.com) - SPF, DKIM, DMARC, and blacklist lookups
- [dmarcian.com](https://dmarcian.com) - DMARC record checker and report analyzer
- [mail-tester.com](https://www.mail-tester.com) - send a real test email and get a full deliverability score covering all four checks together

---

## Propagation and patience

DNS changes are not instant. Expect:

- **A/PTR records:** minutes to a few hours
- **TXT records (SPF/DKIM/DMARC):** minutes to a few hours, occasionally up to 24-48h for some registrars with high TTLs

If `stinger doctor` reports a record missing right after you set it, wait and re-run rather than assuming something's broken - `dig` against a public resolver like `1.1.1.1` directly will often show propagation status faster than your local resolver cache:

```bash
dig @1.1.1.1 A mail.yourdomain.com +short
```

---

## Summary checklist

- [ ] A record: `mail.yourdomain.com` → server IP
- [ ] PTR record: server IP → `mail.yourdomain.com` (set at hosting provider)
- [ ] SPF: single TXT record on root domain, includes your sending IP
- [ ] DKIM: keypair generated, public key published, private key configured in your mail software
- [ ] DMARC: TXT record on `_dmarc.yourdomain.com`, start with `p=none`
- [ ] All records verified with `dig` and/or `stinger doctor`