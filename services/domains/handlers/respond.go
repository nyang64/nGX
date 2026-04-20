/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	dbpkg "agentmail/pkg/db"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case dbpkg.IsNotFound(err):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case dbpkg.IsDuplicateKey(err):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "domain already registered"})
	default:
		slog.Error("internal error", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body: " + err.Error()})
		return false
	}
	return true
}
