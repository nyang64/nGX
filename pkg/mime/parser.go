/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package mime

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
)

// Parse parses a raw RFC 5322 email from r into a ParsedEmail.
func Parse(r io.Reader) (*ParsedEmail, error) {
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}

	parsed := &ParsedEmail{
		Headers: make(map[string][]string),
	}
	for k, v := range msg.Header {
		parsed.Headers[k] = v
	}

	// Standard headers
	parsed.MessageID = strings.Trim(msg.Header.Get("Message-ID"), "<>")
	parsed.InReplyTo = strings.Trim(msg.Header.Get("In-Reply-To"), "<>")
	parsed.Subject = decodeHeader(msg.Header.Get("Subject"))

	// References
	if refs := msg.Header.Get("References"); refs != "" {
		for _, ref := range strings.Fields(refs) {
			parsed.References = append(parsed.References, strings.Trim(ref, "<>"))
		}
	}

	// Date
	if dateStr := msg.Header.Get("Date"); dateStr != "" {
		if t, err := mail.ParseDate(dateStr); err == nil {
			parsed.Date = t
		}
	}

	// From
	if from, err := mail.ParseAddress(msg.Header.Get("From")); err == nil {
		parsed.From = EmailAddress{Email: from.Address, Name: from.Name}
	}

	// Reply-To
	if rt := msg.Header.Get("Reply-To"); rt != "" {
		if rta, err := mail.ParseAddress(rt); err == nil {
			parsed.ReplyTo = rta.Address
		}
	}

	parsed.To = parseAddressList(msg.Header.Get("To"))
	parsed.CC = parseAddressList(msg.Header.Get("Cc"))

	// Body
	contentType := msg.Header.Get("Content-Type")
	if err := parseBody(msg.Body, contentType, parsed); err != nil {
		return nil, fmt.Errorf("parse body: %w", err)
	}

	return parsed, nil
}

func parseBody(r io.Reader, contentType string, parsed *ParsedEmail) error {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// Fallback: treat as plain text
		data, _ := io.ReadAll(r)
		parsed.BodyText = data
		return nil
	}

	switch {
	case mediaType == "text/plain":
		data, err := decodeContent(r, "")
		if err != nil {
			return err
		}
		parsed.BodyText = data

	case mediaType == "text/html":
		data, err := decodeContent(r, "")
		if err != nil {
			return err
		}
		parsed.BodyHTML = data

	case strings.HasPrefix(mediaType, "multipart/"):
		boundary := params["boundary"]
		if boundary == "" {
			return fmt.Errorf("multipart missing boundary")
		}
		mr := multipart.NewReader(r, boundary)
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if err := parsePart(part, parsed); err != nil {
				return err
			}
		}

	default:
		data, _ := io.ReadAll(r)
		parsed.Parts = append(parsed.Parts, Part{
			ContentType: mediaType,
			Data:        data,
		})
	}
	return nil
}

func parsePart(part *multipart.Part, parsed *ParsedEmail) error {
	contentType := part.Header.Get("Content-Type")
	disposition := part.Header.Get("Content-Disposition")
	contentID := strings.Trim(part.Header.Get("Content-Id"), "<>")
	cte := part.Header.Get("Content-Transfer-Encoding")

	mediaType, params, _ := mime.ParseMediaType(contentType)
	dispType, dispParams, _ := mime.ParseMediaType(disposition)

	filename := dispParams["filename"]
	if filename == "" {
		filename = params["name"]
	}
	filename = decodeHeader(filename)

	isInline := dispType == "inline" || contentID != ""

	data, err := decodePartContent(part, cte)
	if err != nil {
		return err
	}

	switch {
	case mediaType == "text/plain" && filename == "" && !isInline:
		parsed.BodyText = data

	case mediaType == "text/html" && filename == "" && !isInline:
		parsed.BodyHTML = data

	case strings.HasPrefix(mediaType, "multipart/"):
		boundary := params["boundary"]
		mr := multipart.NewReader(bytes.NewReader(data), boundary)
		for {
			subpart, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if err := parsePart(subpart, parsed); err != nil {
				return err
			}
		}

	default:
		parsed.Parts = append(parsed.Parts, Part{
			ContentType: mediaType,
			Filename:    filename,
			ContentID:   contentID,
			IsInline:    isInline,
			Data:        data,
		})
	}
	return nil
}

func decodePartContent(r io.Reader, cte string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(r))
	case "base64":
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		// Strip whitespace that mail agents insert for line-wrapping
		data = bytes.ReplaceAll(data, []byte("\r\n"), nil)
		data = bytes.ReplaceAll(data, []byte("\n"), nil)
		dst := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
		n, err := base64.StdEncoding.Decode(dst, data)
		return dst[:n], err
	default:
		return io.ReadAll(r)
	}
}

func decodeContent(r io.Reader, cte string) ([]byte, error) {
	return decodePartContent(r, cte)
}

func parseAddressList(s string) []EmailAddress {
	if s == "" {
		return nil
	}
	addrs, err := mail.ParseAddressList(s)
	if err != nil {
		return nil
	}
	result := make([]EmailAddress, len(addrs))
	for i, a := range addrs {
		result[i] = EmailAddress{Email: a.Address, Name: a.Name}
	}
	return result
}

func decodeHeader(s string) string {
	dec := mime.WordDecoder{}
	decoded, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return decoded
}
