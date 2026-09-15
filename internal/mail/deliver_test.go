package mail

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// stubRelay speaks just enough SMTP to accept one message the way an
// unauthenticated internal relay on port 25 does, and hands back what it
// received.
func stubRelay(t *testing.T) (string, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	received := make(chan string, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		reader := bufio.NewReader(connection)
		write := func(line string) { _, _ = connection.Write([]byte(line + "\r\n")) }
		write("220 relay.corp.example ESMTP")
		var data strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			command := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(command, "EHLO"):
				write("250-relay.corp.example")
				write("250 SIZE 10485760")
			case strings.HasPrefix(command, "MAIL FROM"), strings.HasPrefix(command, "RCPT TO"):
				write("250 OK")
			case command == "DATA":
				write("354 End data with <CR><LF>.<CR><LF>")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
					data.WriteString(line)
				}
				write("250 OK queued")
			case command == "QUIT":
				write("221 Bye")
				received <- data.String()
				return
			default:
				write("500 unknown")
			}
		}
	}()
	return listener.Addr().String(), received
}

func TestDeliverSpeaksSMTPToAnUnauthenticatedRelay(t *testing.T) {
	address, received := stubRelay(t)
	host, port, _ := net.SplitHostPort(address)
	config := ReadConfig(map[string]any{"enabled": true, "smtp_host": host, "smtp_port": port, "from_address": "jikim@corp.example", "from_name": "지킴"}, "")
	config.Port = atoi(port)
	config.Security = "none"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Deliver(ctx, config, Message{To: "alice@corp.example", Subject: "승인 요청", Body: "본문"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	select {
	case raw := <-received:
		for _, want := range []string{"To: alice@corp.example", "Subject: =?utf-8?q?", "X-Jikim-Notification: 1", "본문"} {
			if !strings.Contains(raw, want) {
				t.Fatalf("relay did not receive %q:\n%s", want, raw)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("relay never received the message")
	}
}

func TestDeliverReportsAnUnreachableRelayWithinTheTimeout(t *testing.T) {
	// A closed port on loopback refuses immediately; the point is that the
	// error names the connection step and the call returns.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	host, port, _ := net.SplitHostPort(address)
	config := ReadConfig(map[string]any{"enabled": true, "smtp_host": host, "from_address": "jikim@corp.example", "timeout_seconds": 1}, "")
	config.Port = atoi(port)
	err = Deliver(context.Background(), config, Message{To: "alice@corp.example", Subject: "x", Body: "y"})
	if err == nil || !strings.Contains(err.Error(), "SMTP 연결 실패") {
		t.Fatalf("err=%v, want a connection failure", err)
	}
}

func atoi(value string) int {
	n := 0
	for _, r := range value {
		n = n*10 + int(r-'0')
	}
	return n
}
