/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package handlers

import (
	"net/http"

	"agentmail/pkg/auth"
	"agentmail/services/domains/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// DomainHandler handles HTTP requests for custom domain management.
type DomainHandler struct {
	svc *service.DomainService
}

func NewDomainHandler(svc *service.DomainService) *DomainHandler {
	return &DomainHandler{svc: svc}
}

// Register handles POST /v1/domains
//
// Request:
//
//	{"domain": "acme.com", "pod_id": "<uuid>"}
//
// Response 201:
//
//	{
//	  "domain": {...},
//	  "dns_records": [
//	    {"type":"TXT",   "name":"_amazonses.acme.com",              "value":"<token>",           "purpose":"SES domain ownership verification"},
//	    {"type":"MX",    "name":"acme.com",                          "value":"10 inbound-smtp.us-east-1.amazonaws.com", "purpose":"Route inbound email to SES"},
//	    {"type":"CNAME", "name":"<tok>._domainkey.acme.com",         "value":"<tok>.dkim.amazonses.com", "purpose":"DKIM email signing"},
//	    {"type":"CNAME", "name":"<tok>._domainkey.acme.com",         "value":"<tok>.dkim.amazonses.com", "purpose":"DKIM email signing"},
//	    {"type":"CNAME", "name":"<tok>._domainkey.acme.com",         "value":"<tok>.dkim.amazonses.com", "purpose":"DKIM email signing"}
//	  ]
//	}
func (h *DomainHandler) Register(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var body struct {
		Domain string     `json:"domain"`
		PodID  *uuid.UUID `json:"pod_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain is required"})
		return
	}

	result, err := h.svc.Register(r.Context(), claims, service.RegisterRequest{
		Domain: body.Domain,
		PodID:  body.PodID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// List handles GET /v1/domains
func (h *DomainHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	domains, err := h.svc.List(r.Context(), claims)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

// Get handles GET /v1/domains/{domain_id}
func (h *DomainHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	domainID, err := uuid.Parse(chi.URLParam(r, "domain_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid domain_id"})
		return
	}

	d, err := h.svc.Get(r.Context(), claims, domainID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// Verify handles POST /v1/domains/{domain_id}/verify
// Polls SES for current verification state and updates DB status.
func (h *DomainHandler) Verify(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	domainID, err := uuid.Parse(chi.URLParam(r, "domain_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid domain_id"})
		return
	}

	result, err := h.svc.Verify(r.Context(), claims, domainID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Delete handles DELETE /v1/domains/{domain_id}
func (h *DomainHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	domainID, err := uuid.Parse(chi.URLParam(r, "domain_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid domain_id"})
		return
	}

	if err := h.svc.Delete(r.Context(), claims, domainID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
