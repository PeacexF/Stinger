package verify

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PeacexF/Stinger/internal/smtp"
)

type Status string

const (
	Valid    Status = "valid"
	Invalid  Status = "invalid"
	CatchAll Status = "catch_all"
	Unknown  Status = "unknown"
	Error    Status = "error"
)

// Statuses lists every status in display order
var Statuses = []Status{Valid, CatchAll, Invalid, Unknown, Error}

type SubStatus string

const (
	// valid
	Confirmed SubStatus = "confirmed" // clean 250/251

	// catch-all
	SubCatchAll SubStatus = "catch_all" // domain accepts any

	// invalid
	MailboxNotFound SubStatus = "mailbox_not_found" // 550/551 — user does not exist
	MailboxFull     SubStatus = "mailbox_full"      // 552 — over quota
	DomainRejected  SubStatus = "domain_rejected"   // 553 — bad sender/rcpt at domain level
	SpamBlock       SubStatus = "spam_block"        // 554 — policy/reputation rejection
	SyntaxError     SubStatus = "syntax_error"      // 501 — malformed address
	NoMX            SubStatus = "no_mx"             // NXDOMAIN / no MX records
	Malformed       SubStatus = "malformed"         // empty label, bad format

	// unknown
	Greylisted    SubStatus = "greylisted"     // 451 — try again later
	RateLimited   SubStatus = "rate_limited"   // 421 — too many connections
	MailboxTemp   SubStatus = "mailbox_temp"   // 450 — mailbox temporarily unavailable
	ConnectFailed SubStatus = "connect_failed" // TCP timeout / connection refused
	DNSTimeout    SubStatus = "dns_timeout"    // DNS query timed out
	DNSError      SubStatus = "dns_error"      // other DNS failure
	WorkerError   SubStatus = "worker_error"   // probe failed internally
	TempFailure   SubStatus = "temp_failure"   // other 4xx not specifically classified
)

// Result is one line of results.jsonl
type Result struct {
	Email            string     `json:"email"`
	Status           Status     `json:"status"`
	SubStatus        *SubStatus `json:"sub_status"`
	SMTPCode         *int       `json:"smtp_code"`
	SMTPMessage      *string    `json:"smtp_message"`
	MXUsed           *string    `json:"mx_used"`
	TLSUsed          bool       `json:"tls_used"`
	IsCatchAllDomain bool       `json:"is_catch_all_domain"`
	Attempts         int        `json:"attempts"`
	DurationMS       int64      `json:"duration_ms"`
	Reason           *string    `json:"reason"`
	Timestamp        string     `json:"timestamp"`
}

func (r Result) JSON() ([]byte, error) { return json.Marshal(r) }

func timestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000-07:00")
}

func simpleResult(email string, status Status, sub SubStatus, reason string) Result {
	return Result{
		Email:     email,
		Status:    status,
		SubStatus: &sub,
		Reason:    &reason,
		Timestamp: timestamp(),
	}
}

func probeResult(email string, status Status, sub SubStatus, reason string,
	res smtp.Result, mx string, isCatchAll bool, attempts int) Result {
	r := simpleResult(email, status, sub, reason)
	code, msg := res.SMTPCode, res.SMTPMsg
	r.SMTPCode = &code
	r.SMTPMessage = &msg
	r.MXUsed = &mx
	r.TLSUsed = res.TLSUsed
	r.IsCatchAllDomain = isCatchAll
	r.Attempts = attempts
	r.DurationMS = res.DurationMS
	return r
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// Classify maps a probe result to a status, sub-status and reason
func Classify(res smtp.Result) (Status, SubStatus, string) {
	code, errMsg, msg := res.SMTPCode, res.Error, res.SMTPMsg

	// Network / probe errors
	if errMsg != "" && code == 0 {
		low := strings.ToLower(errMsg)
		switch {
		case containsAny(low, "timeout", "timed out", "deadline"):
			return Unknown, ConnectFailed, "connection timed out: " + errMsg
		case containsAny(low, "refused", "connect failed"):
			return Unknown, ConnectFailed, "connection refused: " + errMsg
		case strings.Contains(low, "worker"):
			return Unknown, WorkerError, "worker error: " + errMsg
		}
		return Unknown, ConnectFailed, "network error: " + errMsg
	}

	switch {
	case code == 250 || code == 251:
		return Valid, Confirmed, msg

	// permanent
	case code == 550 || code == 551:
		return Invalid, MailboxNotFound, "user not found: " + msg
	case code == 552:
		return Invalid, MailboxFull, "mailbox full: " + msg
	case code == 553:
		return Invalid, DomainRejected, "domain rejected: " + msg
	case code == 554:
		// 554 can be either a spam/reputation block or a generic perm failure
		if containsAny(strings.ToLower(msg), "spam", "policy", "blocked", "blacklist", "dnsbl", "reputation") {
			return Invalid, SpamBlock, "spam/policy block: " + msg
		}
		return Invalid, DomainRejected, "permanent rejection: " + msg
	case code == 501:
		return Invalid, SyntaxError, "syntax error: " + msg

	// retryable
	case code == 421:
		return Unknown, RateLimited, "rate limited (421): " + msg
	case code == 450:
		return Unknown, MailboxTemp, "mailbox temp unavailable (450): " + msg
	case code == 451:
		// 451 is the canonical greylisting response
		return Unknown, Greylisted, "greylisted (451): " + msg
	case code == 452:
		return Unknown, MailboxFull, "mailbox full / temp (452): " + msg
	case code == 503:
		return Unknown, TempFailure, "sequence error (503): " + msg
	case code >= 400 && code < 500:
		return Unknown, TempFailure, fmt.Sprintf("temp failure %d: %s", code, msg)
	case code >= 500 && code < 600:
		return Invalid, DomainRejected, fmt.Sprintf("perm rejection %d: %s", code, msg)
	}

	return Unknown, TempFailure, fmt.Sprintf("unclassified: code=%d err=%s", code, errMsg)
}
