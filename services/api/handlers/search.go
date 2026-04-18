/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package handlers

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// SearchHandler proxies search requests to the search service.
type SearchHandler struct {
	searchURL string
	http      *http.Client
}

// NewSearchHandler creates a SearchHandler pointed at searchServiceURL.
func NewSearchHandler(searchServiceURL string) *SearchHandler {
	return &SearchHandler{
		searchURL: searchServiceURL,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// Search proxies GET /v1/search to the search service.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	target := fmt.Sprintf("%s/search?%s", h.searchURL, r.URL.RawQuery)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp("failed to build search request"))
		return
	}
	// Forward the Authorization header — search service validates it directly.
	req.Header.Set("Authorization", r.Header.Get("Authorization"))

	resp, err := h.http.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp("search service unavailable"))
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp("failed to read search response"))
		return
	}
	writeProxied(w, resp.StatusCode, body)
}
