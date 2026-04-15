//go:build smoke

package inbound

import (
	"io"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"testing"

	gosmtp "github.com/emersion/go-smtp"
)

// ---------------------------------------------------------------------------
// Capturing SMTP backend — records every received message without S3/Kafka.
// ---------------------------------------------------------------------------

type capturedMsg struct {
	from string
	to   []string
	body string
}

type capSession struct {
	mu   *sync.Mutex
	msgs *[]capturedMsg
	cur  capturedMsg
}

func (s *capSession) Mail(from string, _ *gosmtp.MailOptions) error {
	s.cur.from = from
	return nil
}

func (s *capSession) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	s.cur.to = append(s.cur.to, to)
	return nil
}

func (s *capSession) Data(r io.Reader) error {
	b, _ := io.ReadAll(r)
	s.cur.body = string(b)
	s.mu.Lock()
	*s.msgs = append(*s.msgs, s.cur)
	s.mu.Unlock()
	return nil
}

func (s *capSession) Reset()        { s.cur = capturedMsg{} }
func (s *capSession) Logout() error { return nil }

type capBackend struct {
	mu   sync.Mutex
	msgs []capturedMsg
}

func (b *capBackend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &capSession{mu: &b.mu, msgs: &b.msgs}, nil
}

// startCaptureSMTP starts an in-process go-smtp server on a random port and
// returns the backend (to inspect captured messages) and the listen address.
func startCaptureSMTP(t *testing.T) (*capBackend, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	be := &capBackend{}
	srv := gosmtp.NewServer(be)
	srv.Domain = "localhost"
	srv.AllowInsecureAuth = true
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return be, ln.Addr().String()
}

// captured returns a snapshot of received messages (thread-safe).
func (b *capBackend) captured() []capturedMsg {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]capturedMsg, len(b.msgs))
	copy(out, b.msgs)
	return out
}

// ---------------------------------------------------------------------------
// Inbound smoke tests — verify the SMTP wire protocol end-to-end.
// ---------------------------------------------------------------------------

// TestSmoke_Inbound_PlainText sends a plain-text email via net/smtp and
// verifies the server captures the correct envelope and body.
func TestSmoke_Inbound_PlainText(t *testing.T) {
	be, addr := startCaptureSMTP(t)

	rawMsg := "From: sender@example.com\r\n" +
		"To: inbox@example.com\r\n" +
		"Subject: Smoke test plain text\r\n" +
		"\r\n" +
		"Hello from the inbound smoke test."

	if err := smtp.SendMail(addr, nil, "sender@example.com", []string{"inbox@example.com"}, []byte(rawMsg)); err != nil {
		t.Fatalf("SendMail: %v", err)
	}

	msgs := be.captured()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 captured message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.from != "sender@example.com" {
		t.Errorf("MAIL FROM: got %q, want %q", m.from, "sender@example.com")
	}
	if len(m.to) != 1 || m.to[0] != "inbox@example.com" {
		t.Errorf("RCPT TO: got %v, want [inbox@example.com]", m.to)
	}
	if !strings.Contains(m.body, "Hello from the inbound smoke test.") {
		t.Errorf("body missing expected content; got:\n%s", m.body)
	}
	if !strings.Contains(m.body, "Subject: Smoke test plain text") {
		t.Errorf("body missing Subject header; got:\n%s", m.body)
	}
}

// TestSmoke_Inbound_MultiRecipient verifies that multiple RCPT TO addresses
// are all recorded in the SMTP envelope.
func TestSmoke_Inbound_MultiRecipient(t *testing.T) {
	be, addr := startCaptureSMTP(t)

	rawMsg := "From: sender@example.com\r\n" +
		"To: alice@example.com, bob@example.com\r\n" +
		"Cc: carol@example.com\r\n" +
		"Subject: Multi-recipient smoke test\r\n" +
		"\r\n" +
		"Sent to multiple recipients."

	to := []string{"alice@example.com", "bob@example.com", "carol@example.com"}
	if err := smtp.SendMail(addr, nil, "sender@example.com", to, []byte(rawMsg)); err != nil {
		t.Fatalf("SendMail: %v", err)
	}

	msgs := be.captured()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].to) != 3 {
		t.Errorf("RCPT TO count: got %d, want 3; recipients: %v", len(msgs[0].to), msgs[0].to)
	}
	rcptSet := map[string]bool{}
	for _, r := range msgs[0].to {
		rcptSet[r] = true
	}
	for _, want := range to {
		if !rcptSet[want] {
			t.Errorf("missing recipient %q in SMTP envelope", want)
		}
	}
}

// TestSmoke_Inbound_MultipartMIME sends a multipart/alternative email and
// verifies both the plain-text and HTML parts arrive intact.
func TestSmoke_Inbound_MultipartMIME(t *testing.T) {
	be, addr := startCaptureSMTP(t)

	rawMsg := strings.Join([]string{
		"From: sender@example.com",
		"To: inbox@example.com",
		"Subject: Multipart smoke test",
		"MIME-Version: 1.0",
		`Content-Type: multipart/alternative; boundary="smoke42"`,
		"",
		"--smoke42",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Plain text smoke content",
		"--smoke42",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>HTML smoke content</p>",
		"--smoke42--",
	}, "\r\n")

	if err := smtp.SendMail(addr, nil, "sender@example.com", []string{"inbox@example.com"}, []byte(rawMsg)); err != nil {
		t.Fatalf("SendMail: %v", err)
	}

	msgs := be.captured()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	body := msgs[0].body
	for _, want := range []string{
		"multipart/alternative",
		"Plain text smoke content",
		"<p>HTML smoke content</p>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

// TestSmoke_Inbound_SequentialMessages verifies the server handles multiple
// messages on separate connections without cross-contamination.
func TestSmoke_Inbound_SequentialMessages(t *testing.T) {
	be, addr := startCaptureSMTP(t)

	emails := []struct{ from, to, subject string }{
		{"alpha@example.com", "inbox@example.com", "First message"},
		{"beta@example.com", "inbox@example.com", "Second message"},
		{"gamma@example.com", "inbox@example.com", "Third message"},
	}

	for _, e := range emails {
		rawMsg := "From: " + e.from + "\r\nTo: " + e.to + "\r\nSubject: " + e.subject + "\r\n\r\nBody of " + e.subject + "."
		if err := smtp.SendMail(addr, nil, e.from, []string{e.to}, []byte(rawMsg)); err != nil {
			t.Fatalf("SendMail from %s: %v", e.from, err)
		}
	}

	msgs := be.captured()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	for i, e := range emails {
		if msgs[i].from != e.from {
			t.Errorf("msg[%d] from: got %q, want %q", i, msgs[i].from, e.from)
		}
		if !strings.Contains(msgs[i].body, e.subject) {
			t.Errorf("msg[%d] body missing subject %q", i, e.subject)
		}
	}
}
