/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"strconv"
	"time"
)

// Client calls an OpenAI-compatible embedding server such as Infinity, Ollama,
// or Cloudflare Workers AI.
type Client struct {
	baseURL string
	model   string
	apiKey  string
	dims    int
	http    *http.Client
}

// New creates a Client.
//
//   - baseURL: e.g. "http://infinity:7997" or Cloudflare AI base URL
//   - model:   e.g. "@cf/baai/bge-base-en-v1.5"
//   - apiKey:  Bearer token (empty = no Authorization header)
//   - dims:    dimensions to keep after truncation (0 = keep all returned dims)
func New(baseURL, model, apiKey string, dims int) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		dims:    dims,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed generates an embedding vector for text, truncated to c.dims dimensions.
// It uses the /embeddings endpoint (Infinity-compatible; Ollama users should
// set EMBEDDER_URL to the /v1 base, e.g. http://localhost:11434/v1).
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{Model: c.model, Input: text})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding server returned %d", resp.StatusCode)
	}

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	vec := result.Data[0].Embedding
	if c.dims > 0 && len(vec) > c.dims {
		vec = vec[:c.dims]
	}
	return vec, nil
}

// VectorLiteral formats a float32 slice as a PostgreSQL vector literal
// suitable for use as a query parameter with a ::vector cast, e.g.:
//
//	UPDATE messages SET embedding = $1::vector WHERE id = $2
func VectorLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strconv.FormatFloat(float64(f), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
