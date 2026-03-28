package emailauth

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	mkdkim "github.com/emersion/go-msgauth/dkim"
)

// VerifyDKIM checks all DKIM-Signature headers in rawMsg.
// Returns DKIMPass if at least one signature is valid, DKIMFail if signatures
// are present but all fail, DKIMNone if no signatures are present.
func VerifyDKIM(rawMsg []byte) DKIMResult {
	verifications, err := mkdkim.Verify(bytes.NewReader(rawMsg))
	if err != nil {
		if mkdkim.IsTempFail(err) {
			return DKIMTempErr
		}
		return DKIMFail
	}
	if len(verifications) == 0 {
		return DKIMNone
	}
	for _, v := range verifications {
		if v.Err == nil {
			return DKIMPass
		}
	}
	return DKIMFail
}

// DKIMSigner holds a parsed private key and signing options ready for use.
type DKIMSigner struct {
	options *mkdkim.SignOptions
}

// NewDKIMSigner parses a PEM-encoded RSA or Ed25519 private key and returns a
// DKIMSigner. Returns nil (not an error) when privateKeyPEM is empty, allowing
// callers to skip signing when DKIM is not configured.
func NewDKIMSigner(privateKeyPEM, selector, domain string) (*DKIMSigner, error) {
	if strings.TrimSpace(privateKeyPEM) == "" {
		return nil, nil
	}
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse DKIM private key: %w", err)
	}
	opts := &mkdkim.SignOptions{
		Domain:   domain,
		Selector: selector,
		Signer:   key,
		// Sign the most important headers. "From" is mandatory per RFC 6376.
		HeaderKeys: []string{
			"From", "To", "Subject", "Date", "Message-ID",
			"Content-Type", "MIME-Version", "In-Reply-To", "References",
		},
	}
	return &DKIMSigner{options: opts}, nil
}

// Sign adds a DKIM-Signature header to msg and returns the signed message.
func (s *DKIMSigner) Sign(msg []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := mkdkim.Sign(&out, bytes.NewReader(msg), s.options); err != nil {
		return nil, fmt.Errorf("DKIM sign: %w", err)
	}
	return out.Bytes(), nil
}

func parsePrivateKey(pemStr string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemStr)))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in DKIM private key")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key does not implement crypto.Signer")
		}
		return signer, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}
}
