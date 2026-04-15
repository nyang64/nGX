//go:build smoke

package outbound

import (
	"io"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"testing"

	"agentmail/services/email-pipeline/emailauth"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Minimal capturing SMTP sink — records every DATA payload delivered to it.
// ---------------------------------------------------------------------------

type sinkMsg struct {
	from string
	to   []string
	data string
}

type sinkSession struct {
	mu   *sync.Mutex
	msgs *[]sinkMsg
	cur  sinkMsg
}

func (s *sinkSession) Mail(from string, _ *gosmtp.MailOptions) error {
	s.cur.from = from
	return nil
}

func (s *sinkSession) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	s.cur.to = append(s.cur.to, to)
	return nil
}

func (s *sinkSession) Data(r io.Reader) error {
	b, _ := io.ReadAll(r)
	s.cur.data = string(b)
	s.mu.Lock()
	*s.msgs = append(*s.msgs, s.cur)
	s.mu.Unlock()
	return nil
}

func (s *sinkSession) Reset()        { s.cur = sinkMsg{} }
func (s *sinkSession) Logout() error { return nil }

type sinkBackend struct {
	mu   sync.Mutex
	msgs []sinkMsg
}

func (b *sinkBackend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &sinkSession{mu: &b.mu, msgs: &b.msgs}, nil
}

func (b *sinkBackend) captured() []sinkMsg {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]sinkMsg, len(b.msgs))
	copy(out, b.msgs)
	return out
}

// startSinkSMTP starts an in-process SMTP sink on a random port.
// Returns the backend (for inspecting captured messages) and the relay address.
func startSinkSMTP(t *testing.T) (*sinkBackend, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	be := &sinkBackend{}
	srv := gosmtp.NewServer(be)
	srv.Domain = "localhost"
	srv.AllowInsecureAuth = true
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return be, ln.Addr().String()
}

// deliver sends pre-built MIME bytes to the sink via net/smtp (same path as
// sender.go when relayHost is set).
func deliver(t *testing.T, addr, from string, to []string, msg []byte) {
	t.Helper()
	if err := smtp.SendMail(addr, nil, from, to, msg); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Outbound smoke tests
// ---------------------------------------------------------------------------

// TestSmoke_Outbound_PlainText verifies that a plain-text email is delivered
// via the relay path with the correct SMTP envelope and MIME headers.
func TestSmoke_Outbound_PlainText(t *testing.T) {
	be, addr := startSinkSMTP(t)
	s := NewSender(nil, nil, addr)

	job := SendJob{
		MessageID: uuid.New(),
		OrgID:     uuid.New(),
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		Subject:   "Smoke: plain text outbound",
		BodyText:  "This is the plain text body.",
	}
	if err := s.Send(t.Context(), job); err != nil {
		t.Fatalf("Send: %v", err)
	}

	msgs := be.captured()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 delivered message, got %d", len(msgs))
	}
	m := msgs[0]

	if m.from != "sender@example.com" {
		t.Errorf("MAIL FROM: got %q, want %q", m.from, "sender@example.com")
	}
	if len(m.to) != 1 || m.to[0] != "recipient@example.com" {
		t.Errorf("RCPT TO: got %v, want [recipient@example.com]", m.to)
	}
	for _, want := range []string{
		"From: sender@example.com",
		"To: recipient@example.com",
		"Subject: Smoke: plain text outbound",
		"MIME-Version: 1.0",
		"text/plain",
		"This is the plain text body.",
	} {
		if !strings.Contains(m.data, want) {
			t.Errorf("delivered message missing %q", want)
		}
	}
}

// TestSmoke_Outbound_MultipartAlternative verifies that a text+HTML email
// is delivered as multipart/alternative with both parts intact.
func TestSmoke_Outbound_MultipartAlternative(t *testing.T) {
	be, addr := startSinkSMTP(t)
	s := NewSender(nil, nil, addr)

	job := SendJob{
		MessageID: uuid.New(),
		OrgID:     uuid.New(),
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		Subject:   "Smoke: multipart alternative",
		BodyText:  "Plain text version.",
		BodyHTML:  "<p>HTML version.</p>",
	}
	if err := s.Send(t.Context(), job); err != nil {
		t.Fatalf("Send: %v", err)
	}

	msgs := be.captured()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 delivered message, got %d", len(msgs))
	}
	data := msgs[0].data
	for _, want := range []string{
		"multipart/alternative",
		"text/plain",
		"text/html",
		"Plain text version.",
		"<p>HTML version.</p>",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("delivered message missing %q", want)
		}
	}
}

// TestSmoke_Outbound_WithAttachment verifies that an email with an attachment
// is delivered as multipart/mixed with the file base64-encoded.
// buildMIMEMessage is called directly (same package) to construct the message,
// then delivered end-to-end via the SMTP sink.
func TestSmoke_Outbound_WithAttachment(t *testing.T) {
	be, addr := startSinkSMTP(t)

	job := SendJob{
		MessageID: uuid.New(),
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		Subject:   "Smoke: with attachment",
	}
	attachments := []attData{
		{
			ref:  AttachmentRef{ContentType: "text/plain", Filename: "notes.txt"},
			data: []byte("attachment content here"),
		},
	}
	msgBytes, err := buildMIMEMessage(job, []byte("See attached notes."), nil, attachments)
	if err != nil {
		t.Fatalf("buildMIMEMessage: %v", err)
	}

	deliver(t, addr, job.From, job.To, msgBytes)

	msgs := be.captured()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 delivered message, got %d", len(msgs))
	}
	body := msgs[0].data
	for _, want := range []string{
		"multipart/mixed",
		"Content-Disposition: attachment",
		"notes.txt",
		"Content-Transfer-Encoding: base64",
		"See attached notes.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("delivered message missing %q", want)
		}
	}
}

// TestSmoke_Outbound_DKIMSigned verifies that a DKIM-Signature header is
// present in the delivered message when a signer is configured.
func TestSmoke_Outbound_DKIMSigned(t *testing.T) {
	be, addr := startSinkSMTP(t)

	pemKey := generateTestRSAKey(t) // helper defined in sender_test.go (same package)
	signer, err := emailauth.NewDKIMSigner(pemKey, "smoke", "example.com")
	if err != nil {
		t.Fatalf("NewDKIMSigner: %v", err)
	}

	s := NewSender(nil, signer, addr)
	job := SendJob{
		MessageID: uuid.New(),
		OrgID:     uuid.New(),
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		Subject:   "Smoke: DKIM signed",
		BodyText:  "This message should carry a DKIM-Signature.",
	}
	if err := s.Send(t.Context(), job); err != nil {
		t.Fatalf("Send: %v", err)
	}

	msgs := be.captured()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 delivered message, got %d", len(msgs))
	}
	data := msgs[0].data
	if !strings.Contains(data, "DKIM-Signature:") {
		t.Errorf("expected DKIM-Signature header in delivered message; got:\n%.500s", data)
	}
	if !strings.Contains(data, "d=example.com") {
		t.Errorf("DKIM-Signature missing expected domain d=example.com")
	}
}

// TestSmoke_Outbound_CCAndBCC verifies that CC appears in MIME headers and
// BCC appears only in the SMTP envelope (not in the message body).
func TestSmoke_Outbound_CCAndBCC(t *testing.T) {
	be, addr := startSinkSMTP(t)
	s := NewSender(nil, nil, addr)

	job := SendJob{
		MessageID: uuid.New(),
		OrgID:     uuid.New(),
		From:      "sender@example.com",
		To:        []string{"to@example.com"},
		Cc:        []string{"cc@example.com"},
		Bcc:       []string{"bcc@example.com"},
		Subject:   "Smoke: CC and BCC",
		BodyText:  "Testing CC and BCC handling.",
	}
	if err := s.Send(t.Context(), job); err != nil {
		t.Fatalf("Send: %v", err)
	}

	msgs := be.captured()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 delivered message, got %d", len(msgs))
	}
	m := msgs[0]

	// All three must appear in the SMTP RCPT TO envelope.
	rcptSet := map[string]bool{}
	for _, r := range m.to {
		rcptSet[r] = true
	}
	for _, want := range []string{"to@example.com", "cc@example.com", "bcc@example.com"} {
		if !rcptSet[want] {
			t.Errorf("RCPT TO missing %q (got %v)", want, m.to)
		}
	}

	// CC must appear in MIME headers; BCC must not.
	if !strings.Contains(m.data, "Cc: cc@example.com") {
		t.Errorf("MIME headers missing Cc: cc@example.com")
	}
	if strings.Contains(m.data, "bcc@example.com") {
		t.Error("BCC address must not appear in MIME message headers")
	}
}

// TestSmoke_Outbound_ThreadingHeaders verifies that In-Reply-To and References
// headers are present in the delivered MIME message when set on the job.
func TestSmoke_Outbound_ThreadingHeaders(t *testing.T) {
	be, addr := startSinkSMTP(t)
	s := NewSender(nil, nil, addr)

	job := SendJob{
		MessageID:  uuid.New(),
		OrgID:      uuid.New(),
		From:       "sender@example.com",
		To:         []string{"recipient@example.com"},
		Subject:    "Re: Smoke: threading",
		BodyText:   "This is a reply.",
		InReplyTo:  "<original@example.com>",
		References: []string{"<root@example.com>", "<original@example.com>"},
	}
	if err := s.Send(t.Context(), job); err != nil {
		t.Fatalf("Send: %v", err)
	}

	msgs := be.captured()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 delivered message, got %d", len(msgs))
	}
	data := msgs[0].data
	for _, want := range []string{
		"In-Reply-To: <original@example.com>",
		"References: <root@example.com> <original@example.com>",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("delivered message missing threading header %q", want)
		}
	}
}
