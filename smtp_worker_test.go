package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// generate self-signed cert for mock TLS
func generateTestCertificate() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Stinger Testing Group"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(1 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}, nil
}

func TestProbe_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		job           Job
		serverMode    string
		expectedCode  int
		expectedError string
	}{
		{
			name: "Successful Standard Delivery Path",
			job: Job{
				Email:      "target@victim.com",
				MX:         "localhost",
				Helo:       "stinger-probe",
				MailFrom:   "spoof@attacker.com",
				TimeoutSec: 5,
				TryTLS:     false,
			},
			serverMode:    "happy",
			expectedCode:  250,
			expectedError: "",
		},
		{
			name: "Successful STARTTLS Upgrade Sequence",
			job: Job{
				Email:      "target@secure-mx.com",
				MX:         "localhost",
				Helo:       "stinger-probe",
				MailFrom:   "spoof@attacker.com",
				TimeoutSec: 5,
				TryTLS:     true,
			},
			serverMode:    "tls_upgrade",
			expectedCode:  250,
			expectedError: "",
		},
		{
			name: "Server Rejects Mail From Sender Address",
			job: Job{
				Email:      "target@victim.com",
				MX:         "localhost",
				Helo:       "stinger-probe",
				MailFrom:   "blacklisted@bad.com",
				TimeoutSec: 5,
				TryTLS:     false,
			},
			serverMode:    "reject_sender",
			expectedCode:  0,
			expectedError: "MAIL FROM rejected: code=554",
		},
		{
			name: "Invalid Server Banner Code",
			job: Job{
				Email:      "target@victim.com",
				MX:         "localhost",
				TimeoutSec: 5,
			},
			serverMode:    "bad_banner",
			expectedCode:  554,
			expectedError: "unexpected banner code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("failed to start local test listener: %v", err)
			}
			defer listener.Close()

			_, portStr, _ := net.SplitHostPort(listener.Addr().String())
			var allocatedPort int
			fmt.Sscanf(portStr, "%d", &allocatedPort)

			testJob := tt.job
			testJob.Port = allocatedPort

			dialTimeout = func(network, address string, timeout time.Duration) (net.Conn, error) {
				return net.DialTimeout(network, listener.Addr().String(), timeout)
			}

			serverReady := make(chan struct{})

			go func() {
				close(serverReady)

				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()

				reader := bufio.NewReader(conn)

				if tt.serverMode == "bad_banner" {
					fmt.Fprintf(conn, "554 Server overloaded, go away\r\n")
					return
				}
				fmt.Fprintf(conn, "220 mail.victim.com ESMTP Postfix\r\n")

				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}

					cmd := strings.ToUpper(strings.TrimSpace(line))

					switch {
					case strings.HasPrefix(cmd, "EHLO") || strings.HasPrefix(cmd, "HELO"):
						if tt.serverMode == "tls_upgrade" {
							fmt.Fprintf(conn, "250-mail.secure-mx.com Hello\r\n250-STARTTLS\r\n250 HELP\r\n")
						} else {
							fmt.Fprintf(conn, "250-mail.victim.com Hello\r\n250 HELP\r\n")
						}

					case strings.HasPrefix(cmd, "STARTTLS"):
						fmt.Fprintf(conn, "220 2.0.0 Ready to start TLS\r\n")

						// Generate certificate configuration dynamically
						cert, err := generateTestCertificate()
						if err != nil {
							return
						}
						tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}

						tlsConn := tls.Server(conn, tlsConfig)
						if err := tlsConn.Handshake(); err != nil {
							return
						}

						conn = tlsConn
						reader = bufio.NewReader(conn)

					case strings.HasPrefix(cmd, "MAIL FROM"):
						if tt.serverMode == "reject_sender" {
							fmt.Fprintf(conn, "554 5.7.1 Client host blocked\r\n")
							return
						}
						fmt.Fprintf(conn, "250 2.1.0 Sender OK\r\n")

					case strings.HasPrefix(cmd, "RCPT TO"):
						fmt.Fprintf(conn, "250 2.1.5 Recipient OK\r\n")

					case strings.HasPrefix(cmd, "QUIT"):
						fmt.Fprintf(conn, "221 2.0.0 Bye\r\n")
						return
					default:
						fmt.Fprintf(conn, "500 Unknown Command\r\n")
					}
				}
			}()

			<-serverReady

			res := probe(testJob)

			if tt.expectedError == "" {
				if res.Error != "" {
					t.Errorf("expected no error, but got: %q", res.Error)
				}
				if res.SMTPCode != tt.expectedCode {
					t.Errorf("expected SMTP Code %d, got %d", tt.expectedCode, res.SMTPCode)
				}
			} else {
				if !strings.Contains(res.Error, tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, res.Error)
				}
			}
		})
	}
}
