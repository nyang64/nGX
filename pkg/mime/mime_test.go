package mime

import (
	"strings"
	"testing"
)

const simplePlainEmail = `From: Alice <alice@example.com>
To: bob@example.com
Subject: Hello World
Date: Mon, 01 Jan 2024 12:00:00 +0000
Message-ID: <abc123@example.com>
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

Hello, Bob!
`

const multipartEmail = `From: sender@example.com
To: recipient@example.com
Subject: Multipart
Date: Mon, 01 Jan 2024 12:00:00 +0000
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="boundary123"

--boundary123
Content-Type: text/plain; charset=utf-8

Plain text body
--boundary123
Content-Type: text/html; charset=utf-8

<b>HTML body</b>
--boundary123--
`

func TestParse_SimplePlain(t *testing.T) {
	parsed, err := Parse(strings.NewReader(simplePlainEmail))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if parsed.From.Email != "alice@example.com" {
		t.Errorf("From.Email = %q, want %q", parsed.From.Email, "alice@example.com")
	}
	if parsed.From.Name != "Alice" {
		t.Errorf("From.Name = %q, want %q", parsed.From.Name, "Alice")
	}
	if len(parsed.To) != 1 || parsed.To[0].Email != "bob@example.com" {
		t.Errorf("To = %v, want [bob@example.com]", parsed.To)
	}
	if parsed.Subject != "Hello World" {
		t.Errorf("Subject = %q, want %q", parsed.Subject, "Hello World")
	}
	if parsed.MessageID != "abc123@example.com" {
		t.Errorf("MessageID = %q, want %q", parsed.MessageID, "abc123@example.com")
	}
	if !strings.Contains(string(parsed.BodyText), "Hello, Bob!") {
		t.Errorf("BodyText = %q, want to contain 'Hello, Bob!'", parsed.BodyText)
	}
}

func TestParse_Multipart(t *testing.T) {
	parsed, err := Parse(strings.NewReader(multipartEmail))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if !strings.Contains(string(parsed.BodyText), "Plain text body") {
		t.Errorf("expected plain text body, got %q", parsed.BodyText)
	}
	if !strings.Contains(string(parsed.BodyHTML), "HTML body") {
		t.Errorf("expected HTML body, got %q", parsed.BodyHTML)
	}
}

func TestParse_InvalidInput(t *testing.T) {
	_, err := Parse(strings.NewReader("not a valid email"))
	if err == nil {
		t.Error("expected error for invalid email")
	}
}

func TestParse_InReplyTo(t *testing.T) {
	raw := `From: a@example.com
To: b@example.com
Subject: Re: Test
Date: Mon, 01 Jan 2024 12:00:00 +0000
In-Reply-To: <original@example.com>
References: <original@example.com>
Content-Type: text/plain

reply
`
	parsed, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if parsed.InReplyTo != "original@example.com" {
		t.Errorf("InReplyTo = %q, want %q", parsed.InReplyTo, "original@example.com")
	}
	if len(parsed.References) != 1 || parsed.References[0] != "original@example.com" {
		t.Errorf("References = %v", parsed.References)
	}
}

func TestParse_CC(t *testing.T) {
	raw := `From: a@example.com
To: b@example.com
Cc: c@example.com, d@example.com
Subject: CC Test
Date: Mon, 01 Jan 2024 12:00:00 +0000
Content-Type: text/plain

body
`
	parsed, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(parsed.CC) != 2 {
		t.Errorf("expected 2 CC addresses, got %d: %v", len(parsed.CC), parsed.CC)
	}
}

func TestParseAddressList(t *testing.T) {
	tests := []struct {
		input string
		count int
	}{
		{"alice@example.com", 1},
		{"alice@example.com, bob@example.com", 2},
		{"", 0},
		{"invalid-not-an-address", 0},
	}
	for _, tc := range tests {
		got := parseAddressList(tc.input)
		if len(got) != tc.count {
			t.Errorf("parseAddressList(%q): got %d addresses, want %d", tc.input, len(got), tc.count)
		}
	}
}

func TestDecodeHeader_Plain(t *testing.T) {
	got := decodeHeader("Hello World")
	if got != "Hello World" {
		t.Errorf("decodeHeader = %q, want %q", got, "Hello World")
	}
}

func TestDecodeHeader_Encoded(t *testing.T) {
	// =?utf-8?q?Hello_World?= is RFC 2047 encoded "Hello World"
	got := decodeHeader("=?utf-8?q?Hello_World?=")
	if got != "Hello World" {
		t.Errorf("decodeHeader = %q, want %q", got, "Hello World")
	}
}

func TestParse_Base64Attachment(t *testing.T) {
	// multipart/mixed with a base64-encoded attachment part
	raw := "From: a@example.com\r\n" +
		"To: b@example.com\r\n" +
		"Subject: Attachment\r\n" +
		"Date: Mon, 01 Jan 2024 12:00:00 +0000\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"bnd\"\r\n" +
		"\r\n" +
		"--bnd\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"See attachment.\r\n" +
		"--bnd\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"file.bin\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"aGVsbG8=\r\n" + // base64("hello")
		"--bnd--\r\n"

	parsed, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !strings.Contains(string(parsed.BodyText), "See attachment") {
		t.Errorf("expected body text, got %q", parsed.BodyText)
	}
	if len(parsed.Parts) != 1 {
		t.Fatalf("expected 1 attachment part, got %d", len(parsed.Parts))
	}
	if string(parsed.Parts[0].Data) != "hello" {
		t.Errorf("attachment data = %q, want %q", parsed.Parts[0].Data, "hello")
	}
	if parsed.Parts[0].Filename != "file.bin" {
		t.Errorf("filename = %q, want file.bin", parsed.Parts[0].Filename)
	}
}

func TestParse_QuotedPrintablePart(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"To: b@example.com\r\n" +
		"Subject: QP Test\r\n" +
		"Date: Mon, 01 Jan 2024 12:00:00 +0000\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"qpbnd\"\r\n" +
		"\r\n" +
		"--qpbnd\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"Hello =3D World\r\n" + // QP for "Hello = World"
		"--qpbnd--\r\n"

	parsed, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !strings.Contains(string(parsed.BodyText), "Hello = World") {
		t.Errorf("expected decoded QP text, got %q", parsed.BodyText)
	}
}

func TestParse_InlineImage(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"To: b@example.com\r\n" +
		"Subject: Inline\r\n" +
		"Date: Mon, 01 Jan 2024 12:00:00 +0000\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/related; boundary=\"relbnd\"\r\n" +
		"\r\n" +
		"--relbnd\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<img src=cid:img1>\r\n" +
		"--relbnd\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-Disposition: inline\r\n" +
		"Content-Id: <img1>\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"aW1nZGF0YQ==\r\n" + // base64("imgdata")
		"--relbnd--\r\n"

	parsed, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(parsed.Parts) != 1 {
		t.Fatalf("expected 1 inline part, got %d: %v", len(parsed.Parts), parsed.Parts)
	}
	if !parsed.Parts[0].IsInline {
		t.Error("expected IsInline=true")
	}
	if parsed.Parts[0].ContentID != "img1" {
		t.Errorf("ContentID = %q, want img1", parsed.Parts[0].ContentID)
	}
}

func TestParse_TopLevelHTML(t *testing.T) {
	// Message with Content-Type: text/html at the top level (no multipart).
	raw := `From: a@example.com
To: b@example.com
Subject: HTML Only
Date: Mon, 01 Jan 2024 12:00:00 +0000
Content-Type: text/html; charset=utf-8

<p>Hello</p>
`
	parsed, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !strings.Contains(string(parsed.BodyHTML), "<p>Hello</p>") {
		t.Errorf("BodyHTML = %q, want to contain '<p>Hello</p>'", parsed.BodyHTML)
	}
}

func TestParse_UnknownContentType(t *testing.T) {
	// Non-text, non-multipart top-level content type → falls through to default.
	raw := `From: a@example.com
To: b@example.com
Subject: Binary
Date: Mon, 01 Jan 2024 12:00:00 +0000
Content-Type: application/octet-stream

binarydata
`
	parsed, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(parsed.Parts) != 1 {
		t.Fatalf("expected 1 part for unknown content-type, got %d", len(parsed.Parts))
	}
	if parsed.Parts[0].ContentType != "application/octet-stream" {
		t.Errorf("ContentType = %q", parsed.Parts[0].ContentType)
	}
}

func TestParse_ReplyTo(t *testing.T) {
	raw := `From: a@example.com
To: b@example.com
Reply-To: reply@example.com
Subject: Test
Date: Mon, 01 Jan 2024 12:00:00 +0000
Content-Type: text/plain

body
`
	parsed, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if parsed.ReplyTo != "reply@example.com" {
		t.Errorf("ReplyTo = %q, want reply@example.com", parsed.ReplyTo)
	}
}
