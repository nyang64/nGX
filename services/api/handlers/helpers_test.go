package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	authpkg "agentmail/pkg/auth"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]string
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("body[status] = %q, want ok", got["status"])
	}
}

func TestWriteProxied(t *testing.T) {
	w := httptest.NewRecorder()
	writeProxied(w, http.StatusOK, []byte(`{"proxied":true}`))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != `{"proxied":true}` {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestDecode_Valid(t *testing.T) {
	body := bytes.NewReader([]byte(`{"key":"val"}`))
	r := httptest.NewRequest(http.MethodPost, "/", body)
	var dst struct{ Key string }
	if err := decode(r, &dst); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if dst.Key != "val" {
		t.Errorf("Key = %q, want val", dst.Key)
	}
}

func TestDecode_Invalid(t *testing.T) {
	body := bytes.NewReader([]byte("not json"))
	r := httptest.NewRequest(http.MethodPost, "/", body)
	var dst struct{ Key string }
	if err := decode(r, &dst); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestClaimsFromCtx_Set(t *testing.T) {
	orgID := uuid.New()
	claims := &authpkg.Claims{OrgID: orgID}
	ctx := authpkg.WithClaims(context.Background(), claims)
	r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)

	got := claimsFromCtx(r)
	if got == nil {
		t.Fatal("expected non-nil claims")
	}
	if got.OrgID != orgID {
		t.Errorf("OrgID = %v, want %v", got.OrgID, orgID)
	}
}

func TestClaimsFromCtx_Nil(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	got := claimsFromCtx(r)
	if got != nil {
		t.Errorf("expected nil claims for unauthenticated request, got %v", got)
	}
}

func TestPathUUID_Valid(t *testing.T) {
	id := uuid.New()
	r := httptest.NewRequest(http.MethodGet, "/"+id.String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	got, err := pathUUID(r, "id")
	if err != nil {
		t.Fatalf("pathUUID error: %v", err)
	}
	if got != id {
		t.Errorf("got %v, want %v", got, id)
	}
}

func TestPathUUID_Invalid(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/bad", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-a-uuid")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	_, err := pathUUID(r, "id")
	if err == nil {
		t.Error("expected error for invalid UUID")
	}
}
