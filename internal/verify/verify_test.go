package verify

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PeacexF/Stinger/internal/config"
	"github.com/PeacexF/Stinger/internal/resolver"
	"github.com/PeacexF/Stinger/internal/smtp"
)

func TestValidEmail(t *testing.T) {
	for _, e := range []string{"alice@example.com", "a.b+c@sub.example.co.uk", "user_name123@example.io"} {
		if !ValidEmail(e) {
			t.Errorf("ValidEmail(%q) = false, want true", e)
		}
	}
	for _, e := range []string{"not-an-email", "@example.com", "alice@", "alice@@example.com",
		"alice@.com", "", "alice@.bk.ru", "alice@b..com"} {
		if ValidEmail(e) {
			t.Errorf("ValidEmail(%q) = true, want false", e)
		}
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		res        smtp.Result
		status     Status
		sub        SubStatus
		reasonPart string
	}{
		{smtp.Result{SMTPCode: 250, SMTPMsg: "2.1.5 OK"}, Valid, Confirmed, "2.1.5 OK"},
		{smtp.Result{SMTPCode: 251, SMTPMsg: "forwarding"}, Valid, Confirmed, ""},
		{smtp.Result{SMTPCode: 550, SMTPMsg: "user unknown"}, Invalid, MailboxNotFound, "user unknown"},
		{smtp.Result{SMTPCode: 551, SMTPMsg: "user unknown"}, Invalid, MailboxNotFound, ""},
		{smtp.Result{SMTPCode: 552, SMTPMsg: "quota"}, Invalid, MailboxFull, ""},
		{smtp.Result{SMTPCode: 553, SMTPMsg: "bad addr"}, Invalid, DomainRejected, ""},
		{smtp.Result{SMTPCode: 554, SMTPMsg: "Blocked by SPAM filter"}, Invalid, SpamBlock, ""},
		{smtp.Result{SMTPCode: 554, SMTPMsg: "listed on DNSBL"}, Invalid, SpamBlock, ""},
		{smtp.Result{SMTPCode: 554, SMTPMsg: "transaction failed"}, Invalid, DomainRejected, ""},
		{smtp.Result{SMTPCode: 501, SMTPMsg: "bad syntax"}, Invalid, SyntaxError, ""},
		{smtp.Result{SMTPCode: 421, SMTPMsg: "too many"}, Unknown, RateLimited, ""},
		{smtp.Result{SMTPCode: 450, SMTPMsg: "busy"}, Unknown, MailboxTemp, ""},
		{smtp.Result{SMTPCode: 451, SMTPMsg: "try later"}, Unknown, Greylisted, ""},
		{smtp.Result{SMTPCode: 452, SMTPMsg: "temp full"}, Unknown, MailboxFull, ""},
		{smtp.Result{SMTPCode: 503, SMTPMsg: "bad sequence"}, Unknown, TempFailure, ""},
		{smtp.Result{SMTPCode: 432, SMTPMsg: "?"}, Unknown, TempFailure, ""},
		{smtp.Result{SMTPCode: 599, SMTPMsg: "?"}, Invalid, DomainRejected, ""},
		{smtp.Result{Error: "connect failed: i/o timeout"}, Unknown, ConnectFailed, "timed out"},
		{smtp.Result{Error: "connect failed: connection refused"}, Unknown, ConnectFailed, "refused"},
		{smtp.Result{Error: "worker crashed"}, Unknown, WorkerError, ""},
		{smtp.Result{Error: "EOF"}, Unknown, ConnectFailed, "network error"},
		{smtp.Result{}, Unknown, TempFailure, "unclassified"},
	}
	for _, tt := range tests {
		status, sub, reason := Classify(tt.res)
		if status != tt.status || sub != tt.sub || !strings.Contains(reason, tt.reasonPart) {
			t.Errorf("Classify(%+v) = (%s, %s, %q), want (%s, %s, ~%q)",
				tt.res, status, sub, reason, tt.status, tt.sub, tt.reasonPart)
		}
	}
}

func TestResultJSONShape(t *testing.T) {
	r := simpleResult("a@b.com", Invalid, NoMX, "no MX")
	b, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	for _, k := range []string{"email", "status", "sub_status", "smtp_code", "smtp_message", "mx_used",
		"tls_used", "is_catch_all_domain", "attempts", "duration_ms", "reason", "timestamp"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %s", k, b)
		}
	}
	if m["smtp_code"] != nil || m["sub_status"] != "no_mx" {
		t.Errorf("unexpected JSON: %s", b)
	}
}

// fake wires a Verifier to a scripted MX lookup and prober
type fake struct {
	mxErr   error
	mxs     []string
	respond func(job smtp.Job) smtp.Result

	mu    sync.Mutex
	calls []smtp.Job
}

func (f *fake) verifier(t *testing.T) *Verifier {
	t.Helper()
	cfg := config.Default()
	cfg.SMTP.HeloHostname = "mail.test"
	cfg.SMTP.MailFrom = "probe@test"
	cfg.Retry.MaxAttempts = 2
	cfg.Retry.BackoffBaseSec = 0
	cache := resolver.NewCache(func(ctx context.Context, domain string) ([]string, error) {
		return f.mxs, f.mxErr
	}, time.Hour, time.Hour)
	return New(&cfg, cache, func(ctx context.Context, job smtp.Job) smtp.Result {
		f.mu.Lock()
		f.calls = append(f.calls, job)
		f.mu.Unlock()
		return f.respond(job)
	})
}

// isCatchAllProbe reports whether job is the random-address catch-all probe
func isCatchAllProbe(job smtp.Job) bool { return strings.HasPrefix(job.Email, catchAllProbePrefix) }

func code(c int) func(smtp.Job) smtp.Result {
	return func(job smtp.Job) smtp.Result {
		if isCatchAllProbe(job) {
			return smtp.Result{SMTPCode: 550}
		}
		return smtp.Result{SMTPCode: c, SMTPMsg: "msg"}
	}
}

func TestVerify_Malformed(t *testing.T) {
	f := &fake{respond: code(250)}
	v := f.verifier(t)
	for _, e := range []string{"no-at-symbol", "bad@@x.com", "a@.bk.ru"} {
		r := v.Verify(context.Background(), e)
		if r.Status != Invalid || *r.SubStatus != Malformed {
			t.Errorf("%q: got %s/%s", e, r.Status, *r.SubStatus)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("expected no probes, got %d", len(f.calls))
	}
}

func TestVerify_Lowercases(t *testing.T) {
	f := &fake{mxs: []string{"mx1.example.com"}, respond: code(250)}
	r := f.verifier(t).Verify(context.Background(), "  Alice@Example.COM ")
	if r.Email != "alice@example.com" {
		t.Errorf("email = %q", r.Email)
	}
}

func TestVerify_DNSErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		mxs    []string
		status Status
		sub    SubStatus
	}{
		{"nxdomain", &net.DNSError{Err: "no such host", IsNotFound: true}, nil, Invalid, NoMX},
		{"empty", nil, []string{}, Invalid, NoMX},
		{"timeout", &net.DNSError{Err: "i/o timeout", IsTimeout: true}, nil, Unknown, DNSTimeout},
		{"servfail", &net.DNSError{Err: "server misbehaving"}, nil, Unknown, DNSError},
		{"other", errors.New("boom"), nil, Unknown, DNSError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fake{mxErr: tt.err, mxs: tt.mxs, respond: code(250)}
			r := f.verifier(t).Verify(context.Background(), "a@example.com")
			if r.Status != tt.status || *r.SubStatus != tt.sub {
				t.Errorf("got %s/%s, want %s/%s", r.Status, *r.SubStatus, tt.status, tt.sub)
			}
		})
	}
}

func TestVerify_Valid(t *testing.T) {
	f := &fake{mxs: []string{"mx1.example.com", "mx2.example.com"}, respond: code(250)}
	r := f.verifier(t).Verify(context.Background(), "a@example.com")
	if r.Status != Valid || *r.SubStatus != Confirmed || *r.MXUsed != "mx1.example.com" {
		t.Errorf("got %s/%s via %s", r.Status, *r.SubStatus, *r.MXUsed)
	}
	if f.calls[0].Helo != "mail.test" || f.calls[0].MailFrom != "probe@test" {
		t.Errorf("job not built from config: %+v", f.calls[0])
	}
}

func TestVerify_InvalidStopsImmediately(t *testing.T) {
	f := &fake{mxs: []string{"mx1", "mx2"}, respond: code(550)}
	r := f.verifier(t).Verify(context.Background(), "a@example.com")
	if r.Status != Invalid || *r.SubStatus != MailboxNotFound || r.Attempts != 1 {
		t.Errorf("got %s/%s attempts=%d", r.Status, *r.SubStatus, r.Attempts)
	}
}

func TestVerify_CatchAll(t *testing.T) {
	f := &fake{mxs: []string{"mx1"}, respond: func(smtp.Job) smtp.Result { return smtp.Result{SMTPCode: 250} }}
	v := f.verifier(t)
	r := v.Verify(context.Background(), "a@example.com")
	if r.Status != CatchAll || *r.SubStatus != SubCatchAll || !r.IsCatchAllDomain {
		t.Errorf("got %s/%s catchAll=%v", r.Status, *r.SubStatus, r.IsCatchAllDomain)
	}
	// Second address on the same domain reuses the cached catch-all result
	f.calls = nil
	v.Verify(context.Background(), "b@example.com")
	if len(f.calls) != 1 {
		t.Errorf("expected 1 probe with cached catch-all, got %d", len(f.calls))
	}
}

func TestVerify_RetriesThenExhausts(t *testing.T) {
	f := &fake{mxs: []string{"mx1"}, respond: code(451)}
	r := f.verifier(t).Verify(context.Background(), "a@example.com")
	if r.Status != Unknown || *r.SubStatus != Greylisted || r.Attempts != 2 {
		t.Errorf("got %s/%s attempts=%d", r.Status, *r.SubStatus, r.Attempts)
	}
}

func TestVerify_FallsBackToSecondMX(t *testing.T) {
	f := &fake{mxs: []string{"mx1", "mx2"}}
	f.respond = func(job smtp.Job) smtp.Result {
		if isCatchAllProbe(job) {
			return smtp.Result{SMTPCode: 550}
		}
		if job.MX == "mx1" {
			return smtp.Result{SMTPCode: 421}
		}
		return smtp.Result{SMTPCode: 250}
	}
	r := f.verifier(t).Verify(context.Background(), "a@example.com")
	if r.Status != Valid || *r.MXUsed != "mx2" {
		t.Errorf("got %s via %s", r.Status, *r.MXUsed)
	}
}

func TestVerify_PerDomainLimit(t *testing.T) {
	var mu sync.Mutex
	inflight, peak := 0, 0
	f := &fake{mxs: []string{"mx1"}}
	f.respond = func(job smtp.Job) smtp.Result {
		mu.Lock()
		inflight++
		peak = max(peak, inflight)
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		inflight--
		mu.Unlock()
		return smtp.Result{SMTPCode: 550}
	}
	v := f.verifier(t) // per_domain_limit defaults to 2
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() { v.Verify(context.Background(), string(rune('a'+i))+"@example.com") })
	}
	wg.Wait()
	if peak > 2 {
		t.Errorf("peak concurrent probes to one domain = %d, want <= 2", peak)
	}
}

func TestSmokeTestDNS(t *testing.T) {
	f := &fake{mxs: []string{"gmail-smtp-in.l.google.com"}}
	if err := f.verifier(t).SmokeTestDNS(context.Background()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	f = &fake{mxs: []string{}}
	if err := f.verifier(t).SmokeTestDNS(context.Background()); err == nil || !strings.Contains(err.Error(), "gmail.com") {
		t.Errorf("expected gmail.com error, got %v", err)
	}
}
