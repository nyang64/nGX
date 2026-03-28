package outbound

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net"
	"strings"
	"testing"

	"agentmail/services/email-pipeline/emailauth"

	"github.com/google/uuid"
)

// generateTestRSAKey returns a PEM-encoded PKCS1 RSA private key for use in tests.
func generateTestRSAKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return string(pem.EncodeToMemory(block))
}

// ---------------------------------------------------------------------------
// Sender.Send — paths reachable without real S3 / SMTP
// ---------------------------------------------------------------------------

func TestSend_NoRecipients(t *testing.T) {
	s := NewSender(nil, nil, "")
	err := s.Send(t.Context(), SendJob{
		MessageID: uuid.New(),
		From:      "a@example.com",
		// No To/Cc/Bcc
	})
	if err == nil || !strings.Contains(err.Error(), "no recipients") {
		t.Errorf("expected 'no recipients' error, got %v", err)
	}
}

func TestSend_RelayHostError(t *testing.T) {
	// TCP server that closes immediately (no SMTP greeting) → smtp.SendMail errors.
	// Covers the s.relayHost != "" branch in Send.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	s := NewSender(nil, nil, ln.Addr().String())
	err = s.Send(t.Context(), SendJob{
		MessageID: uuid.New(),
		From:      "sender@example.com",
		To:        []string{"rcpt@example.com"},
		BodyText:  "hello",
	})
	if err == nil {
		t.Error("expected error from failed SMTP relay")
	}
}

func TestSend_WithDKIMSigningAndRelayError(t *testing.T) {
	// Covers the s.dkimSigner != nil branch: DKIM signing is attempted before delivery.
	pemKey := generateTestRSAKey(t)
	signer, err := emailauth.NewDKIMSigner(pemKey, "test", "example.com")
	if err != nil {
		t.Fatalf("NewDKIMSigner: %v", err)
	}

	// TCP server that closes immediately → smtp.SendMail fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	s := NewSender(nil, signer, ln.Addr().String())
	err = s.Send(t.Context(), SendJob{
		MessageID: uuid.New(),
		From:      "sender@example.com",
		To:        []string{"rcpt@example.com"},
		BodyText:  "hello",
	})
	// Delivery fails (SMTP error) but DKIM code path was executed.
	if err == nil {
		t.Error("expected SMTP error after DKIM signing")
	}
}

func TestSend_InvalidRecipientAddress(t *testing.T) {
	// inline body (no S3 keys), no relay host → falls through to MX path
	// recipient has no "@" → "invalid recipient address" error before DNS lookup
	s := NewSender(nil, nil, "")
	err := s.Send(t.Context(), SendJob{
		MessageID: uuid.New(),
		From:      "sender@example.com",
		To:        []string{"notanemail"},
		BodyText:  "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid recipient address") {
		t.Errorf("expected 'invalid recipient address' error, got %v", err)
	}
}

func TestSend_MXLookupFails(t *testing.T) {
	// A clearly non-existent TLD triggers MX lookup failure.
	s := NewSender(nil, nil, "")
	err := s.Send(t.Context(), SendJob{
		MessageID: uuid.New(),
		From:      "sender@example.com",
		To:        []string{"rcpt@no-such-domain.invalid"},
		BodyText:  "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "MX lookup failed") {
		t.Errorf("expected 'MX lookup failed' error, got %v", err)
	}
}

// --- allRecipients tests ---

func TestAllRecipients_Basic(t *testing.T) {
	job := SendJob{
		To:  []string{"a@x.com"},
		Cc:  []string{"b@x.com"},
		Bcc: []string{"c@x.com"},
	}
	got := allRecipients(job)
	if len(got) != 3 {
		t.Errorf("len: got %d, want 3", len(got))
	}
}

func TestAllRecipients_Dedup(t *testing.T) {
	job := SendJob{
		To:  []string{"a@x.com", "a@x.com"},
		Bcc: []string{"a@x.com"},
	}
	got := allRecipients(job)
	if len(got) != 1 {
		t.Errorf("len: got %d, want 1 (deduplication failed)", len(got))
	}
}

func TestAllRecipients_EmptyStrings(t *testing.T) {
	job := SendJob{
		To: []string{"", "a@x.com"},
	}
	got := allRecipients(job)
	if len(got) != 1 {
		t.Errorf("len: got %d, want 1 (empty strings should be skipped)", len(got))
	}
	if got[0] != "a@x.com" {
		t.Errorf("recipient: got %q, want %q", got[0], "a@x.com")
	}
}

func TestAllRecipients_Empty(t *testing.T) {
	job := SendJob{}
	got := allRecipients(job)
	if len(got) != 0 {
		t.Errorf("len: got %d, want 0", len(got))
	}
}

// --- buildBodyPart tests ---

func TestBuildBodyPart_TextOnly(t *testing.T) {
	result := buildBodyPart([]byte("Hello"), nil)
	if !strings.Contains(result, "text/plain") {
		t.Error("expected 'text/plain' in result")
	}
	if !strings.Contains(result, "Hello") {
		t.Error("expected 'Hello' in result")
	}
	if strings.Contains(result, "text/html") {
		t.Error("unexpected 'text/html' in text-only result")
	}
}

func TestBuildBodyPart_HTMLOnly(t *testing.T) {
	result := buildBodyPart(nil, []byte("<b>hi</b>"))
	if !strings.Contains(result, "text/html") {
		t.Error("expected 'text/html' in result")
	}
	if !strings.Contains(result, "<b>hi</b>") {
		t.Error("expected '<b>hi</b>' in result")
	}
	if strings.Contains(result, "text/plain") {
		t.Error("unexpected 'text/plain' in HTML-only result")
	}
}

func TestBuildBodyPart_Both(t *testing.T) {
	result := buildBodyPart([]byte("plain text"), []byte("<p>html</p>"))
	if !strings.Contains(result, "multipart/alternative") {
		t.Error("expected 'multipart/alternative' in result")
	}
	if !strings.Contains(result, "text/plain") {
		t.Error("expected 'text/plain' in result")
	}
	if !strings.Contains(result, "text/html") {
		t.Error("expected 'text/html' in result")
	}
}

// --- encodeBase64Lines tests ---

func TestEncodeBase64Lines_Short(t *testing.T) {
	input := []byte("short input")
	result := encodeBase64Lines(input)

	// Result must end with "\r\n"
	if !strings.HasSuffix(result, "\r\n") {
		t.Errorf("result does not end with CRLF: %q", result)
	}

	// Strip trailing CRLF and verify the base64 is valid
	trimmed := strings.TrimRight(result, "\r\n")
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		t.Errorf("invalid base64: %v", err)
	}
	if string(decoded) != string(input) {
		t.Errorf("decoded: got %q, want %q", decoded, input)
	}
}

func TestEncodeBase64Lines_Long(t *testing.T) {
	// 100 bytes will produce more than 76 base64 chars
	input := make([]byte, 100)
	for i := range input {
		input[i] = byte(i % 256)
	}
	result := encodeBase64Lines(input)

	lines := strings.Split(result, "\r\n")
	// Last element after trailing \r\n split will be empty string
	for i, line := range lines {
		if line == "" {
			continue // trailing empty after final \r\n
		}
		if len(line) > 76 {
			t.Errorf("line %d has length %d > 76: %q", i, len(line), line)
		}
	}

	// Verify the full base64 decodes correctly
	var combined strings.Builder
	for _, line := range lines {
		combined.WriteString(line)
	}
	decoded, err := base64.StdEncoding.DecodeString(combined.String())
	if err != nil {
		t.Errorf("invalid base64: %v", err)
	}
	if string(decoded) != string(input) {
		t.Error("decoded bytes do not match input")
	}
}

func TestEncodeBase64Lines_Empty(t *testing.T) {
	result := encodeBase64Lines(nil)
	// base64 of empty is ""; the loop does not execute so result is ""
	// OR if implementation writes an empty line: result is "\r\n"
	// Both are acceptable; just ensure no panic and result is well-formed.
	if result != "" && result != "\r\n" {
		t.Errorf("unexpected result for empty input: %q", result)
	}
}

// --- buildMIMEMessage tests ---

func newBasicJob() SendJob {
	return SendJob{
		MessageID: uuid.New(),
		OrgID:     uuid.New(),
		InboxID:   uuid.New(),
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		Subject:   "Test Subject",
	}
}

func TestBuildMIMEMessage_Basic(t *testing.T) {
	job := newBasicJob()
	msg, err := buildMIMEMessage(job, []byte("Hello"), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(msg)
	for _, want := range []string{"From:", "To:", "Subject:", "MIME-Version: 1.0"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in message", want)
		}
	}
}

func TestBuildMIMEMessage_WithCC(t *testing.T) {
	job := newBasicJob()
	job.Cc = []string{"cc@example.com"}
	msg, err := buildMIMEMessage(job, []byte("Hello"), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(msg), "Cc:") {
		t.Error("missing 'Cc:' header")
	}
}

func TestBuildMIMEMessage_WithReplyTo(t *testing.T) {
	job := newBasicJob()
	job.ReplyTo = "replyto@example.com"
	msg, err := buildMIMEMessage(job, []byte("Hello"), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(msg), "Reply-To:") {
		t.Error("missing 'Reply-To:' header")
	}
}

func TestBuildMIMEMessage_WithInReplyTo(t *testing.T) {
	job := newBasicJob()
	job.InReplyTo = "<msg@x.com>"
	msg, err := buildMIMEMessage(job, []byte("Hello"), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(msg), "In-Reply-To:") {
		t.Error("missing 'In-Reply-To:' header")
	}
}

func TestBuildMIMEMessage_WithReferences(t *testing.T) {
	job := newBasicJob()
	job.References = []string{"<a@x>", "<b@x>"}
	msg, err := buildMIMEMessage(job, []byte("Hello"), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(msg), "References:") {
		t.Error("missing 'References:' header")
	}
}

func TestBuildMIMEMessage_WithAttachment(t *testing.T) {
	job := newBasicJob()
	attachments := []attData{
		{
			ref: AttachmentRef{
				ContentType: "image/png",
				Filename:    "test.png",
			},
			data: []byte("pngdata"),
		},
	}
	msg, err := buildMIMEMessage(job, []byte("Hello"), nil, attachments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(msg)
	if !strings.Contains(s, "multipart/mixed") {
		t.Error("missing 'multipart/mixed'")
	}
	if !strings.Contains(s, "Content-Disposition: attachment") {
		t.Error("missing 'Content-Disposition: attachment'")
	}
	if !strings.Contains(s, "test.png") {
		t.Error("missing 'test.png' filename")
	}
}

func TestBuildMIMEMessage_InlineAttachment(t *testing.T) {
	job := newBasicJob()
	attachments := []attData{
		{
			ref: AttachmentRef{
				ContentType: "image/png",
				ContentID:   "img1",
				Inline:      true,
			},
			data: []byte("pngdata"),
		},
	}
	msg, err := buildMIMEMessage(job, []byte("Hello"), nil, attachments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(msg)
	if !strings.Contains(s, "Content-Disposition: inline") {
		t.Error("missing 'Content-Disposition: inline'")
	}
	if !strings.Contains(s, "Content-ID: <img1>") {
		t.Error("missing 'Content-ID: <img1>'")
	}
}

func TestBuildMIMEMessage_CustomHeaders(t *testing.T) {
	job := newBasicJob()
	job.Headers = map[string][]string{
		"X-Custom": {"val1"},
	}
	msg, err := buildMIMEMessage(job, []byte("Hello"), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(msg), "X-Custom: val1") {
		t.Error("missing 'X-Custom: val1' header")
	}
}

func TestBuildMIMEMessage_SkipDuplicateHeaders(t *testing.T) {
	job := newBasicJob()
	job.InReplyTo = "<msg@x.com>"
	// Also pass In-Reply-To via custom Headers — it should be skipped.
	job.Headers = map[string][]string{
		"In-Reply-To": {"<duplicate@x.com>"},
	}
	msg, err := buildMIMEMessage(job, []byte("Hello"), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(msg)
	count := strings.Count(s, "In-Reply-To:")
	if count != 1 {
		t.Errorf("'In-Reply-To:' appears %d times, want exactly 1", count)
	}
}
