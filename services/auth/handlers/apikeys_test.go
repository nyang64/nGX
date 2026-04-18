/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"agentmail/pkg/models"
	"agentmail/services/auth/service"
	"agentmail/services/auth/store"
)

// mockStore is a test double for store.APIKeyStore.
type mockStore struct {
	createFn   func(ctx context.Context, orgID uuid.UUID, name string, scopes []string, podID *uuid.UUID) (*models.APIKey, string, error)
	validateFn func(ctx context.Context, plaintextKey string) (*models.APIKey, error)
	listFn     func(ctx context.Context, orgID uuid.UUID) ([]*models.APIKey, error)
	revokeFn   func(ctx context.Context, orgID, keyID uuid.UUID) error
	getByIDFn  func(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, error)
}

// Ensure mockStore satisfies the interface at compile time.
var _ store.APIKeyStore = (*mockStore)(nil)

func (m *mockStore) Create(ctx context.Context, orgID uuid.UUID, name string, scopes []string, podID *uuid.UUID) (*models.APIKey, string, error) {
	if m.createFn != nil {
		return m.createFn(ctx, orgID, name, scopes, podID)
	}
	return nil, "", nil
}

func (m *mockStore) Validate(ctx context.Context, plaintextKey string) (*models.APIKey, error) {
	if m.validateFn != nil {
		return m.validateFn(ctx, plaintextKey)
	}
	return nil, nil
}

func (m *mockStore) List(ctx context.Context, orgID uuid.UUID) ([]*models.APIKey, error) {
	if m.listFn != nil {
		return m.listFn(ctx, orgID)
	}
	return nil, nil
}

func (m *mockStore) Revoke(ctx context.Context, orgID, keyID uuid.UUID) error {
	if m.revokeFn != nil {
		return m.revokeFn(ctx, orgID, keyID)
	}
	return nil
}

func (m *mockStore) GetByID(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, orgID, keyID)
	}
	return nil, nil
}

// helpers

func newTestKey(orgID, keyID uuid.UUID) *models.APIKey {
	return &models.APIKey{
		ID:        keyID,
		OrgID:     orgID,
		Name:      "test-key",
		KeyPrefix: "am_live_abc",
		Scopes:    []string{"org:admin"},
		CreatedAt: time.Now(),
	}
}

func newHandler(ms *mockStore) *APIKeyHandler {
	svc := service.NewAuthService(ms)
	return NewAPIKeyHandler(svc)
}

// --- Create tests ---

func TestCreate_Success(t *testing.T) {
	orgID := uuid.New()
	keyID := uuid.New()
	plaintext := "am_live_xxx"

	h := newHandler(&mockStore{
		createFn: func(_ context.Context, _ uuid.UUID, _ string, _ []string, _ *uuid.UUID) (*models.APIKey, string, error) {
			return newTestKey(orgID, keyID), plaintext, nil
		},
	})

	body, _ := json.Marshal(map[string]any{
		"org_id": orgID.String(),
		"name":   "test",
		"scopes": []string{"org:admin"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api-keys", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["key"]; !ok {
		t.Error("response missing 'key' field")
	}
	if _, ok := resp["id"]; !ok {
		t.Error("response missing 'id' field")
	}
}

func TestCreate_InvalidBody(t *testing.T) {
	h := newHandler(&mockStore{})
	req := httptest.NewRequest(http.MethodPost, "/api-keys", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

func TestCreate_InvalidOrgID(t *testing.T) {
	h := newHandler(&mockStore{})
	body, _ := json.Marshal(map[string]any{
		"org_id": "not-a-uuid",
		"name":   "test",
		"scopes": []string{"s"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api-keys", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

func TestCreate_InvalidPodID(t *testing.T) {
	h := newHandler(&mockStore{})
	podIDStr := "not-a-uuid"
	body, _ := json.Marshal(map[string]any{
		"org_id": uuid.New().String(),
		"name":   "test",
		"scopes": []string{"s"},
		"pod_id": podIDStr,
	})
	req := httptest.NewRequest(http.MethodPost, "/api-keys", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

func TestCreate_ServiceError(t *testing.T) {
	h := newHandler(&mockStore{
		createFn: func(_ context.Context, _ uuid.UUID, _ string, _ []string, _ *uuid.UUID) (*models.APIKey, string, error) {
			return nil, "", errors.New("db failure")
		},
	})

	body, _ := json.Marshal(map[string]any{
		"org_id": uuid.New().String(),
		"name":   "test",
		"scopes": []string{"org:admin"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api-keys", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}

// --- List tests ---

func TestList_Success(t *testing.T) {
	orgID := uuid.New()
	keys := []*models.APIKey{
		newTestKey(orgID, uuid.New()),
		newTestKey(orgID, uuid.New()),
	}

	h := newHandler(&mockStore{
		listFn: func(_ context.Context, _ uuid.UUID) ([]*models.APIKey, error) {
			return keys, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api-keys?org_id="+orgID.String(), nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["items"]; !ok {
		t.Error("response missing 'items' field")
	}
}

func TestList_InvalidOrgID(t *testing.T) {
	h := newHandler(&mockStore{})
	req := httptest.NewRequest(http.MethodGet, "/api-keys?org_id=bad", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

// --- Revoke tests ---

func withChiKeyID(r *http.Request, keyID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("keyID", keyID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestRevoke_Success(t *testing.T) {
	orgID := uuid.New()
	keyID := uuid.New()

	h := newHandler(&mockStore{
		revokeFn: func(_ context.Context, _, _ uuid.UUID) error {
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api-keys/"+keyID.String()+"?org_id="+orgID.String(), nil)
	req = withChiKeyID(req, keyID.String())
	w := httptest.NewRecorder()

	h.Revoke(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", w.Code)
	}
}

func TestRevoke_InvalidOrgID(t *testing.T) {
	keyID := uuid.New()
	h := newHandler(&mockStore{})

	req := httptest.NewRequest(http.MethodDelete, "/api-keys/"+keyID.String()+"?org_id=bad", nil)
	req = withChiKeyID(req, keyID.String())
	w := httptest.NewRecorder()

	h.Revoke(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

func TestRevoke_InvalidKeyID(t *testing.T) {
	orgID := uuid.New()
	h := newHandler(&mockStore{})

	req := httptest.NewRequest(http.MethodDelete, "/api-keys/bad-uuid?org_id="+orgID.String(), nil)
	req = withChiKeyID(req, "bad-uuid")
	w := httptest.NewRecorder()

	h.Revoke(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

// --- Validate tests ---

func TestValidate_Success(t *testing.T) {
	orgID := uuid.New()
	keyID := uuid.New()

	h := newHandler(&mockStore{
		validateFn: func(_ context.Context, _ string) (*models.APIKey, error) {
			return newTestKey(orgID, keyID), nil
		},
	})

	body, _ := json.Marshal(map[string]string{"key": "am_live_xxx"})
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	// The handler writes Claims as JSON; Claims has OrgID field.
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Claims JSON keys are exported field names: OrgID, KeyID, Scopes, PodID
	if _, ok := resp["OrgID"]; !ok {
		t.Error("response missing 'OrgID' field")
	}
}

func TestValidate_InvalidKey(t *testing.T) {
	h := newHandler(&mockStore{
		validateFn: func(_ context.Context, _ string) (*models.APIKey, error) {
			return nil, errors.New("not found")
		},
	})

	body, _ := json.Marshal(map[string]string{"key": "bad_key"})
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

func TestValidate_InvalidBody(t *testing.T) {
	h := newHandler(&mockStore{})

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	Health(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
}

func TestReady(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	Ready(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
}

func TestList_ServiceError(t *testing.T) {
	ms := &mockStore{
		listFn: func(_ context.Context, _ uuid.UUID) ([]*models.APIKey, error) {
			return nil, errors.New("db error")
		},
	}
	h := newHandler(ms)
	orgID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api-keys?org_id="+orgID.String(), nil)
	w := httptest.NewRecorder()
	h.List(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}

func TestRevoke_ServiceError(t *testing.T) {
	ms := &mockStore{
		revokeFn: func(_ context.Context, _, _ uuid.UUID) error {
			return errors.New("not found")
		},
	}
	h := newHandler(ms)
	orgID := uuid.New()
	keyID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api-keys/"+keyID.String()+"?org_id="+orgID.String(), nil)
	req = withChiKeyID(req, keyID.String())
	w := httptest.NewRecorder()
	h.Revoke(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}
