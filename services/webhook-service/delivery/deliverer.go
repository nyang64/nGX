package delivery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"time"

	"agentmail/pkg/models"
)

// Deliverer performs HTTP POST delivery of webhook payloads.
type Deliverer struct {
	http *http.Client
}

// NewDeliverer creates a Deliverer with a 30-second timeout.
func NewDeliverer() *Deliverer {
	return &Deliverer{
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// DeliveryResult holds the outcome of a single delivery attempt.
type DeliveryResult struct {
	Success      bool
	StatusCode   int
	ResponseBody string
	Error        string
}

// Deliver performs a signed HTTP POST to the webhook's URL with the given payload.
func (d *Deliverer) Deliver(ctx context.Context, webhook *models.Webhook, payload []byte) DeliveryResult {
	signature := computeSignature(webhook.Secret, payload)

	req, err := http.NewRequestWithContext(ctx, "POST", webhook.URL, bytes.NewReader(payload))
	if err != nil {
		return DeliveryResult{Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentMail-Signature", "sha256="+signature)
	req.Header.Set("X-AgentMail-Event", "webhook.delivery")
	req.Header.Set("User-Agent", "AgentMail-Webhook/1.0")
	if webhook.AuthHeaderName != nil && webhook.AuthHeaderValue != "" {
		req.Header.Set(*webhook.AuthHeaderName, webhook.AuthHeaderValue)
	}

	resp, err := d.http.Do(req)
	if err != nil {
		return DeliveryResult{Error: err.Error()}
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	return DeliveryResult{
		Success:      resp.StatusCode >= 200 && resp.StatusCode < 300,
		StatusCode:   resp.StatusCode,
		ResponseBody: string(bodyBytes),
	}
}

// computeSignature computes HMAC-SHA256 of payload using the given secret.
func computeSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
