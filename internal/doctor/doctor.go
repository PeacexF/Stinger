// Package doctor validates the DNS setup (A, PTR, SPF, MX) for the
// configured helo_hostname and mail_from, using the same resolvers as a run.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/PeacexF/Stinger/internal/resolver"
	"github.com/PeacexF/Stinger/internal/ui"
)

func describe(err error, notFound string) string {
	var dnsErr *net.DNSError
	switch {
	case resolver.IsTimeout(err):
		return "DNS query timed out"
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		return notFound
	case errors.As(err, &dnsErr):
		return "DNS server failed to respond: " + dnsErr.Err
	}
	return err.Error()
}

func lookupA(ctx context.Context, r *net.Resolver, host string) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ips, err := r.LookupIP(ctx, "ip4", host)
	if err != nil {
		return "", describe(err, "no A record (NXDOMAIN or no answer)")
	}
	if len(ips) == 0 {
		return "", "no A record present"
	}
	return ips[0].String(), ""
}

func lookupPTR(ctx context.Context, r *net.Resolver, ip string) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	names, err := r.LookupAddr(ctx, ip)
	if err != nil {
		return "", describe(err, "no PTR record")
	}
	if len(names) == 0 {
		return "", "no PTR record present"
	}
	return strings.TrimSuffix(names[0], "."), ""
}

// lookupSPF returns the SPF record, or "" with no error when none exists
func lookupSPF(ctx context.Context, r *net.Resolver, domain string) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	txts, err := r.LookupTXT(ctx, domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return "", "" // no TXT records at all
		}
		return "", describe(err, "")
	}
	for _, t := range txts {
		if strings.HasPrefix(t, "v=spf1") {
			return t, ""
		}
	}
	return "", ""
}

// localIP returns the outbound IPv4 of this machine
func localIP() net.IP {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return nil
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP
}

// Run prints the report to w and returns true when all critical checks pass
func Run(ctx context.Context, w io.Writer, heloHostname, mailFrom string, nameservers []string) bool {
	r := resolver.New(nameservers)
	mailFromDomain := mailFrom
	if i := strings.LastIndexByte(mailFrom, '@'); i >= 0 {
		mailFromDomain = mailFrom[i+1:]
	}

	// Local IP — only used for the A record cross-check, never for SPF suggestions
	local := localIP()
	localPublic := local != nil && local.IsGlobalUnicast() && !local.IsPrivate()

	ok := true
	passed, total := 0, 0
	check := func(label string, pass bool, detail string, critical bool) {
		total++
		icon, status, color := "✓", "OK", ui.Green
		switch {
		case pass:
			passed++
		case critical:
			icon, status, color = "✗", "FAIL", ui.Red
			ok = false
		default:
			icon, status, color = "⚠", "WARN", ui.Yellow
		}
		fmt.Fprintf(w, "  %s  [%-4s]  %s\n", color(icon), status, label)
		fmt.Fprintf(w, "          %s\n", detail)
	}
	section := func(title string) {
		fmt.Fprintf(w, "\n  ── %s %s\n", title, strings.Repeat("─", max(0, 46-len([]rune(title)))))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "  ╔══════════════════════════════════════════════════╗")
	fmt.Fprintln(w, "  ║           SMTP-Stinger — DNS Doctor              ║")
	fmt.Fprintln(w, "  ╚══════════════════════════════════════════════════╝")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  helo_hostname : %s\n", heloHostname)
	fmt.Fprintf(w, "  mail_from     : %s\n", mailFrom)
	fmt.Fprintf(w, "  resolvers     : %s\n", strings.Join(resolver.Nameservers(nameservers), ", "))
	if local != nil && localPublic {
		fmt.Fprintf(w, "  this machine  : %s\n", local)
	} else if local != nil {
		fmt.Fprintf(w, "  this machine  : %s (private — not shown in suggestions)\n", local)
	}

	section("A Record")
	aIP, aErr := lookupA(ctx, r, heloHostname)
	label := "A record for " + heloHostname
	if aIP != "" {
		check(label, true, heloHostname+" → "+aIP, true)
	} else {
		check(label, false, fmt.Sprintf("Lookup failed: %s\n          Add:  %s  A  <YOUR_SERVER_IP>", aErr, heloHostname), true)
	}

	section("PTR / Reverse DNS")
	if aIP != "" {
		ptr, ptrErr := lookupPTR(ctx, r, aIP)
		switch {
		case ptr != "" && strings.EqualFold(ptr, heloHostname):
			check("PTR matches helo_hostname", true, aIP+" → "+ptr, true)
		case ptr != "":
			check("PTR matches helo_hostname", false,
				fmt.Sprintf("%s → %s  (expected %s)\n          Fix in your VPS/host panel under rDNS or Reverse DNS.", aIP, ptr, heloHostname), true)
		default:
			check("PTR record exists", false,
				fmt.Sprintf("Lookup failed: %s\n          Set rDNS/Reverse DNS at your VPS/host panel to: %s", ptrErr, heloHostname), true)
		}

		if localPublic && aIP != local.String() {
			check("A record matches this machine's IP", false,
				fmt.Sprintf("A record → %s, this machine → %s\n          Make sure you're running stinger from the correct server.", aIP, local), false)
		} else if localPublic {
			check("A record matches this machine's IP", true, "Both resolve to "+aIP, true)
		}
	} else {
		check("PTR record", false, "Skipped — A record lookup failed.", false)
	}

	section("SPF Record")
	spf, spfErr := lookupSPF(ctx, r, mailFromDomain)
	label = "SPF record for " + mailFromDomain
	switch {
	case spfErr != "":
		check(label, false, "Lookup failed: "+spfErr, false)
	case spf != "":
		check(label, true, spf, true)
		// Check if the A record IP is covered — only meaningful if we have it
		if aIP != "" && !strings.Contains(spf, "ip4:"+aIP) && !strings.Contains(spf, "+all") {
			check("SPF covers server IP", false,
				fmt.Sprintf("ip4:%s not found in SPF record.\n          Suggested record:  v=spf1 ip4:%s ~all", aIP, aIP), false)
		} else if aIP != "" {
			check("SPF covers server IP", true, "ip4:"+aIP+" found or permissive policy present", true)
		}
	default:
		suggest := "v=spf1 ip4:<YOUR_SERVER_IP> ~all"
		if aIP != "" {
			suggest = "v=spf1 ip4:" + aIP + " ~all"
		}
		check(label, false,
			fmt.Sprintf("No SPF TXT record found.\n          Add TXT record:  %s  %q", mailFromDomain, suggest), false)
	}

	section("mail_from domain MX (optional)")
	mxs, _ := resolver.LookupMX(ctx, r, mailFromDomain)
	label = "MX records for " + mailFromDomain
	if len(mxs) > 0 {
		check(label, true, "Found: "+strings.Join(mxs[:min(3, len(mxs))], ", "), true)
	} else {
		check(label, true, "None found — that's fine, the sender domain doesn't need to receive mail.", true)
	}

	section("Summary")
	if ok {
		fmt.Fprintf(w, "  %s\n", ui.Green("All checks passed"))
	} else {
		fmt.Fprintf(w, "  %s\n", ui.Red("Some checks failed — see above"))
	}
	fmt.Fprintf(w, "  %d/%d checks passed\n\n", passed, total)
	if !ok {
		fmt.Fprint(w, "  Once fixed, re-run:  stinger doctor\n\n")
	}
	return ok
}
