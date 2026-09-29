// Package smtp runs a single RCPT TO probe against an MX host.
package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"
)

type Job struct {
	Email          string
	MX             string
	Helo           string
	MailFrom       string
	Port           int
	ConnectTimeout time.Duration
	CommandTimeout time.Duration
	TryTLS         bool
}

type Result struct {
	Email      string
	MX         string
	SMTPCode   int
	SMTPMsg    string
	Error      string
	TLSUsed    bool
	DurationMS int64
}

// overridden in tests
var dial = func(ctx context.Context, network, addr string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, network, addr)
}

// Probe connects to job.MX and runs EHLO / STARTTLS / MAIL FROM / RCPT TO.
// The returned SMTPCode is the RCPT TO response code; Error is set for
// failures before RCPT TO. Cancelling ctx aborts the conversation.
func Probe(ctx context.Context, job Job) Result {
	if job.Port <= 0 {
		job.Port = 25
	}
	if job.ConnectTimeout <= 0 {
		job.ConnectTimeout = 10 * time.Second
	}
	if job.CommandTimeout <= 0 {
		job.CommandTimeout = 15 * time.Second
	}

	start := time.Now()
	res := probe(ctx, job)
	res.DurationMS = time.Since(start).Milliseconds()
	return res
}

func probe(ctx context.Context, job Job) Result {
	addr := net.JoinHostPort(job.MX, fmt.Sprint(job.Port))

	conn, err := dial(ctx, "tcp", addr, job.ConnectTimeout)
	if err != nil {
		return errResult(job, fmt.Sprintf("connect failed: %v", err))
	}
	// Close the socket on cancellation to unblock any pending read
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	defer func() { conn.Close() }()

	s := &session{conn: conn, timeout: job.CommandTimeout}

	banner, code, err := s.read()
	if err != nil {
		return errResult(job, fmt.Sprintf("banner read failed: %v", err))
	}
	if code != 220 {
		return Result{
			Email:    job.Email,
			MX:       job.MX,
			SMTPCode: code,
			SMTPMsg:  banner,
			Error:    "unexpected banner code",
		}
	}

	ehloResp, ehloCode, err := s.cmd("EHLO " + job.Helo)
	if err != nil || ehloCode != 250 {
		_, heloCode, err2 := s.cmd("HELO " + job.Helo)
		if err2 != nil || heloCode != 250 {
			return errResult(job, fmt.Sprintf("HELO/EHLO failed: code=%d err=%v", heloCode, err2))
		}
	}

	tlsUsed := false
	if job.TryTLS && strings.Contains(strings.ToUpper(ehloResp), "STARTTLS") {
		if _, stCode, err := s.cmd("STARTTLS"); err == nil && stCode == 220 {
			// Opportunistic STARTTLS: many MX certs don't match their hostname
			// and nothing sensitive is sent, so verification is skipped on purpose
			tlsConn := tls.Client(s.conn, &tls.Config{
				ServerName:         job.MX,
				InsecureSkipVerify: true,
			})
			s.conn.SetDeadline(time.Now().Add(s.timeout))
			if err := tlsConn.Handshake(); err != nil {
				return errResult(job, fmt.Sprintf("TLS handshake failed: %v", err))
			}
			s.conn = tlsConn
			conn = tlsConn
			tlsUsed = true
			s.cmd("EHLO " + job.Helo)
		}
	}

	_, mfCode, err := s.cmd(fmt.Sprintf("MAIL FROM:<%s>", job.MailFrom))
	if err != nil || mfCode != 250 {
		return errResult(job, fmt.Sprintf("MAIL FROM rejected: code=%d err=%v", mfCode, err))
	}

	rcptResp, rcptCode, err := s.cmd(fmt.Sprintf("RCPT TO:<%s>", job.Email))
	if err != nil {
		return errResult(job, fmt.Sprintf("RCPT TO error: %v", err))
	}

	s.cmd("QUIT")

	return Result{
		Email:    job.Email,
		MX:       job.MX,
		SMTPCode: rcptCode,
		SMTPMsg:  strings.TrimSpace(rcptResp),
		TLSUsed:  tlsUsed,
	}
}

type session struct {
	conn    net.Conn
	timeout time.Duration
}

func (s *session) cmd(line string) (string, int, error) {
	s.conn.SetDeadline(time.Now().Add(s.timeout))
	if _, err := fmt.Fprintf(s.conn, "%s\r\n", line); err != nil {
		return "", 0, err
	}
	return s.read()
}

// read returns one (possibly multi-line) SMTP response and its code
func (s *session) read() (string, int, error) {
	s.conn.SetDeadline(time.Now().Add(s.timeout))

	var full strings.Builder
	buf := make([]byte, 4096)
	code := 0

	for {
		n, err := s.conn.Read(buf)
		if err != nil {
			if full.Len() > 0 {
				break
			}
			return "", 0, err
		}
		full.Write(buf[:n])

		lines := strings.Split(strings.TrimRight(full.String(), "\r\n"), "\n")
		last := strings.TrimSpace(lines[len(lines)-1])

		if len(last) >= 3 {
			fmt.Sscanf(last[:3], "%d", &code)
			// "250-..." continues, "250 ..." or bare "250" ends the response
			if code > 0 && (len(last) == 3 || last[3] != '-') {
				break
			}
		}
	}

	return strings.TrimSpace(full.String()), code, nil
}

func errResult(job Job, msg string) Result {
	return Result{Email: job.Email, MX: job.MX, Error: msg}
}
