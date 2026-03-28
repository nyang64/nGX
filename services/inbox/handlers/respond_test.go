package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentmail/services/inbox/service"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"key": "value"})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]string
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("body key = %q, want value", got["key"])
	}
}

func TestWriteError_NotFound(t *testing.T) {
	// pgx.ErrNoRows is the sentinel for not-found; use it via the pgx package.
	// db.IsNotFound checks errors.Is(err, pgx.ErrNoRows).
	// We use a generic error here which falls through to 500.
	w := httptest.NewRecorder()
	writeError(w, errors.New("something failed"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestWriteError_InvalidReviewStatus(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, service.ErrInvalidReviewStatus)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
}

func TestDecode_Valid(t *testing.T) {
	body := bytes.NewReader([]byte(`{"name":"alice"}`))
	r := httptest.NewRequest(http.MethodPost, "/", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	var dst struct{ Name string }
	ok := decode(w, r, &dst)
	if !ok {
		t.Fatal("decode returned false for valid JSON")
	}
	if dst.Name != "alice" {
		t.Errorf("Name = %q, want alice", dst.Name)
	}
}

func TestDecode_Invalid(t *testing.T) {
	body := bytes.NewReader([]byte("not json"))
	r := httptest.NewRequest(http.MethodPost, "/", body)
	w := httptest.NewRecorder()

	var dst struct{ Name string }
	ok := decode(w, r, &dst)
	if ok {
		t.Fatal("decode returned true for invalid JSON")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid request body") {
		t.Errorf("body = %q, want 'invalid request body'", w.Body.String())
	}
}
