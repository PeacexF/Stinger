// Persistent daemon: reads jobs from stdin in a loop, writes results to stdout
// One process handles many jobs
//
// Protocol (newline-delimited JSON):
// stdin  -> {"email":"...","mx":"...","helo":"...","mail_from":"...","timeout_sec":10,"try_tls":true}
// stdout <- {"email":"...","mx":"...","smtp_code":250,"smtp_message":"...","error":"","tls_used":true,"duration_ms":42}
// shutdown -> {"shutdown":true}  causes clean exit

package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// for tests
var dialTimeout = net.DialTimeout

type Job struct {
	Email      string `json:"email"`
	MX         string `json:"mx"`
	Helo       string `json:"helo"`
	MailFrom   string `json:"mail_from"`
	TimeoutSec int    `json:"timeout_sec"`
	TryTLS     bool   `json:"try_tls"`
	Port       int    `json:"port"`
	Shutdown   bool   `json:"shutdown"`
}

type Result struct {
	Email      string `json:"email"`
	MX         string `json:"mx"`
	SMTPCode   int    `json:"smtp_code"`
	SMTPMsg    string `json:"smtp_message"`
	Error      string `json:"error"`
	TLSUsed    bool   `json:"tls_used"`
	DurationMS int64  `json:"duration_ms"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	encoder := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var job Job
		if err := json.Unmarshal(line, &job); err != nil {
			encoder.Encode(Result{Error: fmt.Sprintf("decode error: %v", err)})
			continue
		}

		// Clean shutdown signal from Python pool
		if job.Shutdown {
			break
		}

		if job.TimeoutSec <= 0 {
			job.TimeoutSec = 10
		}
		if job.Port <= 0 {
			job.Port = 25
		}

		start := time.Now()
		result := probe(job)
		result.DurationMS = time.Since(start).Milliseconds()

		encoder.Encode(result)
	}
}

// SMTP probe
func probe(job Job) Result {
	timeout := time.Duration(job.TimeoutSec) * time.Second
	addr := fmt.Sprintf("%s:%d", job.MX, job.Port)

	conn, err := dialTimeout("tcp", addr, timeout)
	if err != nil {
		return errResult(job, fmt.Sprintf("connect failed: %v", err))
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	// Banner
	banner, code, err := readResponse(conn)
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

	tlsUsed := false

	// EHLO
	ehloResp, ehloCode, err := sendCmd(conn, fmt.Sprintf("EHLO %s", job.Helo))
	if err != nil || ehloCode != 250 {
		_, heloCode, err2 := sendCmd(conn, fmt.Sprintf("HELO %s", job.Helo))
		if err2 != nil || heloCode != 250 {
			return errResult(job, fmt.Sprintf("HELO/EHLO failed: code=%d err=%v", heloCode, err2))
		}
	}

	// STARTTLS
	if job.TryTLS && strings.Contains(strings.ToUpper(ehloResp), "STARTTLS") {
		_, stCode, err := sendCmd(conn, "STARTTLS")
		if err == nil && stCode == 220 {
			tlsConn := tls.Client(conn, &tls.Config{
				ServerName:         job.MX,
				InsecureSkipVerify: true,
			})
			if err := tlsConn.Handshake(); err == nil {
				conn = tlsConn
				tlsUsed = true
				sendCmd(conn, fmt.Sprintf("EHLO %s", job.Helo))
			}
		}
	}

	// MAIL FROM
	_, mfCode, err := sendCmd(conn, fmt.Sprintf("MAIL FROM:<%s>", job.MailFrom))
	if err != nil || mfCode != 250 {
		return errResult(job, fmt.Sprintf("MAIL FROM rejected: code=%d err=%v", mfCode, err))
	}

	// RCPT TO — the actual check
	rcptResp, rcptCode, err := sendCmd(conn, fmt.Sprintf("RCPT TO:<%s>", job.Email))
	if err != nil {
		return errResult(job, fmt.Sprintf("RCPT TO error: %v", err))
	}

	sendCmd(conn, "QUIT")

	return Result{
		Email:    job.Email,
		MX:       job.MX,
		SMTPCode: rcptCode,
		SMTPMsg:  strings.TrimSpace(rcptResp),
		TLSUsed:  tlsUsed,
	}
}

func sendCmd(conn net.Conn, cmd string) (string, int, error) {
	if _, err := fmt.Fprintf(conn, "%s\r\n", cmd); err != nil {
		return "", 0, err
	}
	return readResponse(conn)
}

func readResponse(conn net.Conn) (string, int, error) {
	var fullMsg strings.Builder
	buf := make([]byte, 4096)
	code := 0

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if fullMsg.Len() > 0 {
				break
			}
			return "", 0, err
		}
		fullMsg.WriteString(string(buf[:n]))

		lines := strings.Split(strings.TrimRight(fullMsg.String(), "\r\n"), "\n")
		lastLine := strings.TrimSpace(lines[len(lines)-1])

		if len(lastLine) >= 4 && lastLine[3] == ' ' {
			fmt.Sscanf(lastLine[:3], "%d", &code)
			break
		}
		if len(lastLine) >= 3 {
			fmt.Sscanf(lastLine[:3], "%d", &code)
			if code > 0 && (len(lastLine) < 4 || lastLine[3] != '-') {
				break
			}
		}
	}

	return strings.TrimSpace(fullMsg.String()), code, nil
}

func errResult(job Job, msg string) Result {
	return Result{Email: job.Email, MX: job.MX, Error: msg}
}
