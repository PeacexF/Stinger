// Package verify orchestrates DNS, catch-all detection, retries and
// concurrency limits for SMTP verification.
package verify

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PeacexF/Stinger/internal/config"
	"github.com/PeacexF/Stinger/internal/resolver"
	"github.com/PeacexF/Stinger/internal/smtp"
)

const catchAllProbePrefix = "stinger-catch_all-checker-XYZ"

var emailRe = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// ValidEmail reports whether email is syntactically acceptable
func ValidEmail(email string) bool {
	if !emailRe.MatchString(email) {
		return false
	}
	domain := email[strings.IndexByte(email, '@')+1:]
	// Reject empty labels such as "a@.b.com" or "a@b..com"
	return !strings.HasPrefix(domain, ".") && !strings.Contains(domain, "..")
}

// Prober performs one SMTP probe
type Prober func(ctx context.Context, job smtp.Job) smtp.Result

type Verifier struct {
	smtpCfg     config.SMTP
	maxAttempts int
	backoffBase time.Duration
	perDomain   int

	dns   *resolver.Cache
	probe Prober

	global chan struct{}

	mu         sync.Mutex
	domainSems map[string]chan struct{}
	catchAllMu map[string]*sync.Mutex
}

func New(cfg *config.Config, dns *resolver.Cache, probe Prober) *Verifier {
	return &Verifier{
		smtpCfg:     cfg.SMTP,
		maxAttempts: max(cfg.Retry.MaxAttempts, 1),
		backoffBase: time.Duration(cfg.Retry.BackoffBaseSec * float64(time.Second)),
		perDomain:   max(cfg.Concurrency.PerDomainLimit, 1),
		dns:         dns,
		probe:       probe,
		global:      make(chan struct{}, max(cfg.Concurrency.GlobalLimit, 1)),
		domainSems:  make(map[string]chan struct{}),
		catchAllMu:  make(map[string]*sync.Mutex),
	}
}

// SmokeTestDNS resolves gmail.com MX as a sanity check before a run
func (v *Verifier) SmokeTestDNS(ctx context.Context) error {
	mxs, err := v.dns.MX(ctx, "gmail.com")
	if err != nil {
		return fmt.Errorf("DNS smoke test failed: %v. Check your resolvers in config.yaml", err)
	}
	if len(mxs) == 0 {
		return fmt.Errorf("DNS smoke test failed: gmail.com returned no MX records. " +
			"Check your resolvers in config.yaml")
	}
	return nil
}

// Verify checks a single address. If ctx is cancelled mid-check the
// returned result is meaningless and the caller should discard it.
func (v *Verifier) Verify(ctx context.Context, email string) Result {
	email = strings.ToLower(strings.TrimSpace(email))

	if !ValidEmail(email) {
		return simpleResult(email, Invalid, Malformed, "malformed email address")
	}
	domain := email[strings.IndexByte(email, '@')+1:]

	mxs, err := v.dns.MX(ctx, domain)
	switch {
	case err != nil && resolver.IsTimeout(err):
		return simpleResult(email, Unknown, DNSTimeout, "DNS lookup timed out")
	case err != nil:
		return simpleResult(email, Unknown, DNSError, "DNS error: "+err.Error())
	case len(mxs) == 0:
		return simpleResult(email, Invalid, NoMX, "no MX records (NXDOMAIN or no answer)")
	}

	isCatchAll := v.detectCatchAll(ctx, domain, mxs)
	return v.probeWithRetry(ctx, email, domain, mxs, isCatchAll)
}

func (v *Verifier) domainSem(domain string) chan struct{} {
	v.mu.Lock()
	defer v.mu.Unlock()
	sem, ok := v.domainSems[domain]
	if !ok {
		sem = make(chan struct{}, v.perDomain)
		v.domainSems[domain] = sem
	}
	return sem
}

func (v *Verifier) catchAllLock(domain string) *sync.Mutex {
	v.mu.Lock()
	defer v.mu.Unlock()
	m, ok := v.catchAllMu[domain]
	if !ok {
		m = &sync.Mutex{}
		v.catchAllMu[domain] = m
	}
	return m
}

func (v *Verifier) detectCatchAll(ctx context.Context, domain string, mxs []string) bool {
	if val, ok := v.dns.CatchAll(domain); ok {
		return val
	}

	lock := v.catchAllLock(domain)
	lock.Lock()
	defer lock.Unlock()

	if val, ok := v.dns.CatchAll(domain); ok {
		return val
	}

	probe := fmt.Sprintf("%s%d@%s", catchAllProbePrefix, time.Now().UnixMilli(), domain)
	for _, mx := range mxs[:min(2, len(mxs))] {
		res := v.smtpOnce(ctx, probe, domain, mx)
		if ctx.Err() != nil {
			return false // don't cache a result from an aborted probe
		}
		if res.SMTPCode == 250 || res.SMTPCode == 251 {
			v.dns.SetCatchAll(domain, true)
			return true
		}
		if res.SMTPCode >= 500 || (res.SMTPCode == 0 && res.Error == "") {
			v.dns.SetCatchAll(domain, false)
			return false
		}
	}

	v.dns.SetCatchAll(domain, false)
	return false
}

// smtpOnce probes under the per-domain and global limits. The domain slot is
// taken first so goroutines queued behind a busy domain don't hold global slots.
func (v *Verifier) smtpOnce(ctx context.Context, email, domain, mx string) smtp.Result {
	sem := v.domainSem(domain)
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return smtp.Result{Email: email, MX: mx, Error: "cancelled"}
	}
	defer func() { <-sem }()

	select {
	case v.global <- struct{}{}:
	case <-ctx.Done():
		return smtp.Result{Email: email, MX: mx, Error: "cancelled"}
	}
	defer func() { <-v.global }()

	return v.probe(ctx, smtp.Job{
		Email:          email,
		MX:             mx,
		Helo:           v.smtpCfg.HeloHostname,
		MailFrom:       v.smtpCfg.MailFrom,
		Port:           v.smtpCfg.Port,
		ConnectTimeout: time.Duration(v.smtpCfg.ConnectTimeoutSec) * time.Second,
		CommandTimeout: time.Duration(v.smtpCfg.CommandTimeoutSec) * time.Second,
		TryTLS:         v.smtpCfg.TryTLS,
	})
}

func (v *Verifier) probeWithRetry(ctx context.Context, email, domain string, mxs []string, isCatchAll bool) Result {
	attempts := 0
	var (
		lastRes    smtp.Result
		lastStatus = Unknown
		lastSub    = TempFailure
		lastReason string
	)

	for attempt := 0; attempt < v.maxAttempts; attempt++ {
		for _, mx := range mxs {
			attempts++
			res := v.smtpOnce(ctx, email, domain, mx)
			status, sub, reason := Classify(res)

			switch status {
			case Valid:
				if isCatchAll {
					status, sub = CatchAll, SubCatchAll
				}
				return probeResult(email, status, sub, reason, res, mx, isCatchAll, attempts)
			case Invalid:
				return probeResult(email, status, sub, reason, res, mx, isCatchAll, attempts)
			}

			lastRes, lastStatus, lastSub, lastReason = res, status, sub, reason
			if ctx.Err() != nil {
				break
			}
		}

		if attempt < v.maxAttempts-1 {
			backoff := time.Duration(float64(v.backoffBase) * math.Pow(2, float64(attempt)))
			if !sleep(ctx, backoff) {
				break
			}
		}
	}

	return probeResult(email, lastStatus, lastSub, lastReason, lastRes, mxs[0], isCatchAll, attempts)
}

// sleep waits for d or until ctx is done; it reports whether the full wait elapsed
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
