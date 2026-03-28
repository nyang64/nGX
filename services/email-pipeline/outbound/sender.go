package outbound

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/google/uuid"

	"agentmail/pkg/s3"
	"agentmail/services/email-pipeline/emailauth"
)

// Sender delivers outbound email messages via direct SMTP to the recipient's MX.
type Sender struct {
	s3Client   *s3.Client
	dkimSigner *emailauth.DKIMSigner // nil if DKIM signing is not configured
	relayHost  string               // when non-empty, bypass MX lookup and route to this host:port
}

// NewSender creates a Sender. dkimSigner may be nil to disable DKIM signing.
// relayHost, if non-empty, overrides MX lookup (e.g. "localhost:1025" for Mailhog).
func NewSender(s3Client *s3.Client, dkimSigner *emailauth.DKIMSigner, relayHost string) *Sender {
	return &Sender{s3Client: s3Client, dkimSigner: dkimSigner, relayHost: relayHost}
}

// AttachmentRef carries the S3 key and metadata for an outbound attachment.
type AttachmentRef struct {
	S3Key       string
	Filename    string
	ContentType string
	ContentID   string
	Inline      bool
}

// SendJob carries everything needed to build and deliver a single outbound message.
type SendJob struct {
	MessageID   uuid.UUID
	OrgID       uuid.UUID
	InboxID     uuid.UUID
	From        string
	To          []string
	Cc          []string
	Bcc         []string // not written to MIME headers, but included in SMTP RCPT TO
	Subject     string
	ReplyTo     string   // Reply-To header if set
	InReplyTo   string   // In-Reply-To header
	References  []string // References header chain
	// BodyText / BodyHTML are used when the content is not in S3.
	BodyText    string
	BodyHTML    string
	// S3 keys override the inline body fields when non-empty.
	TextS3Key   string
	HtmlS3Key   string
	Headers     map[string][]string
	Attachments []AttachmentRef
}

// Send fetches bodies from S3 (if needed), builds the MIME message, and
// delivers it to the recipient's MX server.
func (s *Sender) Send(ctx context.Context, job SendJob) error {
	allRcpts := allRecipients(job)
	if len(allRcpts) == 0 {
		return fmt.Errorf("no recipients")
	}

	// Fetch body content from S3 when S3 keys are provided.
	var textBody, htmlBody []byte

	if job.TextS3Key != "" {
		data, err := s.s3Client.Download(ctx, job.TextS3Key)
		if err != nil {
			return fmt.Errorf("fetch text body from S3: %w", err)
		}
		textBody = data
	} else {
		textBody = []byte(job.BodyText)
	}

	if job.HtmlS3Key != "" {
		data, err := s.s3Client.Download(ctx, job.HtmlS3Key)
		if err != nil {
			return fmt.Errorf("fetch html body from S3: %w", err)
		}
		htmlBody = data
	} else {
		htmlBody = []byte(job.BodyHTML)
	}

	// Download attachment data from S3.
	var attachments []attData
	for _, att := range job.Attachments {
		data, err := s.s3Client.Download(ctx, att.S3Key)
		if err != nil {
			slog.Error("failed to fetch attachment from S3, skipping",
				"message_id", job.MessageID, "s3_key", att.S3Key, "error", err)
			continue
		}
		attachments = append(attachments, attData{ref: att, data: data})
	}

	// Build the MIME message.
	msgBytes, err := buildMIMEMessage(job, textBody, htmlBody, attachments)
	if err != nil {
		return fmt.Errorf("build mime message: %w", err)
	}

	// DKIM-sign the message when a signer is configured.
	if s.dkimSigner != nil {
		signed, err := s.dkimSigner.Sign(msgBytes)
		if err != nil {
			// Signing failure is non-fatal: deliver unsigned rather than drop the message.
			slog.Error("DKIM signing failed, sending unsigned", "message_id", job.MessageID, "error", err)
		} else {
			msgBytes = signed
		}
	}

	// Determine delivery target: relay host overrides MX lookup (used in dev/test).
	if s.relayHost != "" {
		return smtp.SendMail(s.relayHost, nil, job.From, allRcpts, msgBytes)
	}

	// Look up MX record for the first To recipient's domain.
	firstRcpt := allRcpts[0]
	parts := strings.SplitN(firstRcpt, "@", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid recipient address: %s", firstRcpt)
	}
	domain := parts[1]

	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		return fmt.Errorf("MX lookup failed for %s: %w", domain, err)
	}

	mxHost := strings.TrimSuffix(mxRecords[0].Host, ".")

	return deliverSMTP(job.From, allRcpts, msgBytes, mxHost)
}

// allRecipients merges To + Cc + Bcc into a single deduplicated SMTP RCPT TO list.
func allRecipients(job SendJob) []string {
	seen := make(map[string]bool)
	var out []string
	for _, addr := range append(append(job.To, job.Cc...), job.Bcc...) {
		if addr != "" && !seen[addr] {
			seen[addr] = true
			out = append(out, addr)
		}
	}
	return out
}

// deliverSMTP attempts to deliver msg to mxHost, preferring direct TLS on port
// 465 and falling back to STARTTLS on port 25.
func deliverSMTP(from string, to []string, msg []byte, mxHost string) error {
	// Try implicit TLS first.
	tlsAddr := mxHost + ":465"
	conn, err := tls.Dial("tcp", tlsAddr, &tls.Config{ServerName: mxHost})
	if err == nil {
		defer conn.Close()

		client, err := smtp.NewClient(conn, mxHost)
		if err != nil {
			return fmt.Errorf("smtp client (TLS): %w", err)
		}
		defer client.Close()

		return sendViaClient(client, from, to, msg)
	}

	// Fallback: plain SMTP with STARTTLS on port 25.
	plainAddr := mxHost + ":25"
	return smtp.SendMail(plainAddr, nil, from, to, msg)
}

// sendViaClient performs the SMTP conversation on an already-connected client.
func sendViaClient(client *smtp.Client, from string, to []string, msg []byte) error {
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA command: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write data: %w", err)
	}
	return w.Close()
}

type attData struct {
	ref  AttachmentRef
	data []byte
}

// buildMIMEMessage assembles an RFC 5322 message from the job and body content.
func buildMIMEMessage(job SendJob, textBody, htmlBody []byte, attachments []attData) ([]byte, error) {
	var buf bytes.Buffer

	// Standard address headers.
	buf.WriteString("From: " + job.From + "\r\n")
	buf.WriteString("To: " + strings.Join(job.To, ", ") + "\r\n")
	if len(job.Cc) > 0 {
		buf.WriteString("Cc: " + strings.Join(job.Cc, ", ") + "\r\n")
	}
	if job.ReplyTo != "" {
		buf.WriteString("Reply-To: " + job.ReplyTo + "\r\n")
	}
	buf.WriteString("Subject: " + job.Subject + "\r\n")
	buf.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")

	// Threading headers.
	if job.InReplyTo != "" {
		buf.WriteString("In-Reply-To: " + job.InReplyTo + "\r\n")
	}
	if len(job.References) > 0 {
		buf.WriteString("References: " + strings.Join(job.References, " ") + "\r\n")
	}

	// Extra headers (e.g. Message-ID, custom headers). Skip threading headers already
	// written above to avoid duplicates.
	skip := map[string]bool{
		"In-Reply-To": true,
		"References":  true,
		"Reply-To":    true,
	}
	for k, vals := range job.Headers {
		if skip[k] {
			continue
		}
		for _, v := range vals {
			buf.WriteString(k + ": " + v + "\r\n")
		}
	}

	// Build the body part(s).
	bodyPart := buildBodyPart(textBody, htmlBody)

	if len(attachments) == 0 {
		// No attachments: write body inline.
		buf.WriteString(bodyPart)
	} else {
		// Wrap body + attachments in multipart/mixed.
		mixedBoundary := fmt.Sprintf("mixed_%d", time.Now().UnixNano())
		buf.WriteString("Content-Type: multipart/mixed; boundary=\"" + mixedBoundary + "\"\r\n")
		buf.WriteString("\r\n")

		buf.WriteString("--" + mixedBoundary + "\r\n")
		buf.WriteString(bodyPart)

		for _, att := range attachments {
			buf.WriteString("--" + mixedBoundary + "\r\n")
			buf.WriteString("Content-Type: " + att.ref.ContentType + "\r\n")
			if att.ref.Inline && att.ref.ContentID != "" {
				buf.WriteString("Content-Disposition: inline\r\n")
				buf.WriteString("Content-ID: <" + att.ref.ContentID + ">\r\n")
			} else {
				buf.WriteString("Content-Disposition: attachment; filename=\"" + att.ref.Filename + "\"\r\n")
			}
			buf.WriteString("Content-Transfer-Encoding: base64\r\n")
			buf.WriteString("\r\n")
			encoded := encodeBase64Lines(att.data)
			buf.WriteString(encoded)
			buf.WriteString("\r\n")
		}

		buf.WriteString("--" + mixedBoundary + "--\r\n")
	}

	return buf.Bytes(), nil
}

// buildBodyPart returns the Content-Type header + blank line + body for the
// text/html content, as a string ready to be written into a MIME part.
func buildBodyPart(textBody, htmlBody []byte) string {
	var buf bytes.Buffer
	hasText := len(textBody) > 0
	hasHTML := len(htmlBody) > 0

	switch {
	case hasText && hasHTML:
		boundary := fmt.Sprintf("alt_%d", time.Now().UnixNano())
		buf.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
		buf.WriteString("\r\n")

		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.Write(textBody)
		buf.WriteString("\r\n")

		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString("Content-Type: text/html; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		buf.Write(htmlBody)
		buf.WriteString("\r\n")

		buf.WriteString("--" + boundary + "--\r\n")

	case hasHTML:
		buf.WriteString("Content-Type: text/html; charset=utf-8\r\n")
		buf.WriteString("\r\n")
		buf.Write(htmlBody)

	default:
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("\r\n")
		buf.Write(textBody)
	}
	return buf.String()
}

// encodeBase64Lines encodes data as base64 with 76-char line wrapping per RFC 2045.
func encodeBase64Lines(data []byte) string {
	const lineLen = 76
	encoded := base64.StdEncoding.EncodeToString(data)
	var buf bytes.Buffer
	for len(encoded) > 0 {
		if len(encoded) > lineLen {
			buf.WriteString(encoded[:lineLen])
			buf.WriteString("\r\n")
			encoded = encoded[lineLen:]
		} else {
			buf.WriteString(encoded)
			buf.WriteString("\r\n")
			break
		}
	}
	return buf.String()
}
