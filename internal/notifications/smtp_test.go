package notifications

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestEncryptedSecretsAreTenantBound(t *testing.T) {
	s := NewService(nil, []byte("01234567890123456789012345678901"), false)
	encoded, err := s.seal(7, "private-smtp-password")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-smtp-password") {
		t.Fatal("plaintext stored")
	}
	decoded, err := s.unseal(7, encoded)
	if err != nil || decoded != "private-smtp-password" {
		t.Fatal("decrypt", err)
	}
	if _, err = s.unseal(8, encoded); err == nil {
		t.Fatal("ciphertext accepted for another tenant")
	}
	s.key[0]++
	if _, err = s.unseal(7, encoded); err == nil {
		t.Fatal("wrong key accepted")
	}
}
func TestSMTPActualAcceptanceAndLostReply(t *testing.T) {
	for _, loseReply := range []bool{false, true} {
		t.Run(strconv.FormatBool(loseReply), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			var mu sync.Mutex
			var received string
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				writer := bufio.NewWriter(conn)
				reader := bufio.NewReader(conn)
				reply := func(text string) { _, _ = writer.WriteString(text + "\r\n"); _ = writer.Flush() }
				reply("220 localhost SMTP ready")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						reply("250 localhost")
					case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
						reply("250 ok")
					case strings.HasPrefix(line, "DATA"):
						reply("354 data")
						var body strings.Builder
						for {
							line, err = reader.ReadString('\n')
							if err != nil {
								return
							}
							if line == ".\r\n" {
								break
							}
							body.WriteString(line)
						}
						mu.Lock()
						received = body.String()
						mu.Unlock()
						if loseReply {
							return
						}
						reply("250 accepted")
					case strings.HasPrefix(line, "QUIT"):
						reply("221 bye")
						return
					default:
						reply("250 ok")
					}
				}
			}()
			_, portText, _ := net.SplitHostPort(listener.Addr().String())
			port, _ := strconv.Atoi(portText)
			s := NewService(nil, nil, true)
			code := s.send(context.Background(), SMTPConfig{Host: "127.0.0.1", Port: port, TLSMode: "NONE", From: "sender@example.test", Version: 1}, "", "user@example.test", "Rozwiązano incydent", 9007199254740999, 17)
			<-done
			if loseReply && code != "SMTP_RESULT_UNKNOWN" || !loseReply && code != "" {
				t.Fatal("unexpected SMTP outcome", code)
			}
			mu.Lock()
			defer mu.Unlock()
			if !strings.Contains(received, "9007199254740999") || !strings.Contains(received, "Message-ID: <opensnow-17-1@example.test>") {
				t.Fatal("real SMTP data not accepted", received)
			}
		})
	}
}
func TestSMTPValidation(t *testing.T) {
	for _, address := range []string{"bad", "x@example.test\r\nBcc: leaked@example.test", "Name <a@example.test>"} {
		if validAddress(address) {
			t.Fatal("unsafe address accepted")
		}
	}
	if !validAddress("valid@example.test") {
		t.Fatal("valid email rejected")
	}
	s := NewService(nil, nil, false)
	if got := s.send(context.Background(), SMTPConfig{TLSMode: "NONE"}, "", "", "", 0, 0); got != "SMTP_INSECURE_DISABLED" {
		t.Fatal(got)
	}
}
