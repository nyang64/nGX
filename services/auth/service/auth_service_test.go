package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	authpkg "agentmail/pkg/auth"
	"agentmail/pkg/models"
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

func newKey(orgID, keyID uuid.UUID, scopes []string) *models.APIKey {
	return &models.APIKey{
		ID:        keyID,
		OrgID:     orgID,
		Name:      "test-key",
		KeyPrefix: "am_live_abc",
		Scopes:    scopes,
		CreatedAt: time.Now(),
	}
}

// TestCreateKey_Success verifies that a valid request returns the key model and plaintext.
func TestCreateKey_Success(t *testing.T) {
	orgID := uuid.New()
	keyID := uuid.New()
	expectedPlaintext := "am_live_xxx"
	expectedKey := newKey(orgID, keyID, []string{"org:admin"})

	svc := NewAuthService(&mockStore{
		createFn: func(_ context.Context, _ uuid.UUID, _ string, _ []string, _ *uuid.UUID) (*models.APIKey, string, error) {
			return expectedKey, expectedPlaintext, nil
		},
	})

	key, plaintext, err := svc.CreateKey(context.Background(), CreateKeyRequest{
		OrgID:  orgID,
		Name:   "test-key",
		Scopes: []string{"org:admin"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("expected key, got nil")
	}
	if key.ID != keyID {
		t.Errorf("key ID: got %v, want %v", key.ID, keyID)
	}
	if plaintext != expectedPlaintext {
		t.Errorf("plaintext: got %q, want %q", plaintext, expectedPlaintext)
	}
}

// TestCreateKey_EmptyScopes verifies that an empty scopes slice returns an error mentioning "scope".
func TestCreateKey_EmptyScopes(t *testing.T) {
	svc := NewAuthService(&mockStore{})

	_, _, err := svc.CreateKey(context.Background(), CreateKeyRequest{
		OrgID:  uuid.New(),
		Name:   "test-key",
		Scopes: []string{},
	})
	if err == nil {
		t.Fatal("expected error for empty scopes, got nil")
	}
	if !strings.Contains(err.Error(), "scope") {
		t.Errorf("error %q does not contain 'scope'", err.Error())
	}
}

// TestCreateKey_StoreError verifies that a store error is propagated.
func TestCreateKey_StoreError(t *testing.T) {
	storeErr := errors.New("db unavailable")
	svc := NewAuthService(&mockStore{
		createFn: func(_ context.Context, _ uuid.UUID, _ string, _ []string, _ *uuid.UUID) (*models.APIKey, string, error) {
			return nil, "", storeErr
		},
	})

	_, _, err := svc.CreateKey(context.Background(), CreateKeyRequest{
		OrgID:  uuid.New(),
		Name:   "test-key",
		Scopes: []string{"org:admin"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestValidateKey_Success verifies that a valid key returns Claims with the correct fields.
func TestValidateKey_Success(t *testing.T) {
	orgID := uuid.New()
	keyID := uuid.New()
	apiKey := newKey(orgID, keyID, []string{"org:admin"})

	svc := NewAuthService(&mockStore{
		validateFn: func(_ context.Context, _ string) (*models.APIKey, error) {
			return apiKey, nil
		},
	})

	claims, err := svc.ValidateKey(context.Background(), "am_live_xxx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.OrgID != orgID {
		t.Errorf("OrgID: got %v, want %v", claims.OrgID, orgID)
	}
	if claims.KeyID != keyID {
		t.Errorf("KeyID: got %v, want %v", claims.KeyID, keyID)
	}
	if len(claims.Scopes) != 1 || claims.Scopes[0] != authpkg.ScopeOrgAdmin {
		t.Errorf("Scopes: got %v, want [org:admin]", claims.Scopes)
	}
	if claims.PodID != nil {
		t.Errorf("PodID: got %v, want nil", claims.PodID)
	}
}

// TestValidateKey_StoreError verifies that a store error causes ValidateKey to return an error.
func TestValidateKey_StoreError(t *testing.T) {
	svc := NewAuthService(&mockStore{
		validateFn: func(_ context.Context, _ string) (*models.APIKey, error) {
			return nil, errors.New("not found")
		},
	})

	_, err := svc.ValidateKey(context.Background(), "bad_key")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestRevokeKey_Success verifies that a successful revocation returns no error.
func TestRevokeKey_Success(t *testing.T) {
	svc := NewAuthService(&mockStore{
		revokeFn: func(_ context.Context, _, _ uuid.UUID) error {
			return nil
		},
	})

	err := svc.RevokeKey(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRevokeKey_Error verifies that a store error is propagated.
func TestRevokeKey_Error(t *testing.T) {
	svc := NewAuthService(&mockStore{
		revokeFn: func(_ context.Context, _, _ uuid.UUID) error {
			return errors.New("not found")
		},
	})

	err := svc.RevokeKey(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestListKeys_Success verifies that the correct number of keys is returned.
func TestListKeys_Success(t *testing.T) {
	orgID := uuid.New()
	keys := []*models.APIKey{
		newKey(orgID, uuid.New(), []string{"org:admin"}),
		newKey(orgID, uuid.New(), []string{"inbox:read"}),
	}

	svc := NewAuthService(&mockStore{
		listFn: func(_ context.Context, _ uuid.UUID) ([]*models.APIKey, error) {
			return keys, nil
		},
	})

	result, err := svc.ListKeys(context.Background(), orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("len: got %d, want 2", len(result))
	}
}

// TestListKeys_Error verifies that a store error is propagated.
func TestListKeys_Error(t *testing.T) {
	svc := NewAuthService(&mockStore{
		listFn: func(_ context.Context, _ uuid.UUID) ([]*models.APIKey, error) {
			return nil, errors.New("db error")
		},
	})

	_, err := svc.ListKeys(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestGetKey_Success verifies that the correct key is returned.
func TestGetKey_Success(t *testing.T) {
	orgID := uuid.New()
	keyID := uuid.New()
	expected := newKey(orgID, keyID, []string{"org:admin"})

	svc := NewAuthService(&mockStore{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.APIKey, error) {
			return expected, nil
		},
	})

	key, err := svc.GetKey(context.Background(), orgID, keyID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil || key.ID != keyID {
		t.Errorf("key ID: got %v, want %v", key, keyID)
	}
}

// TestGetKey_Error verifies that a store error is propagated.
func TestGetKey_Error(t *testing.T) {
	svc := NewAuthService(&mockStore{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*models.APIKey, error) {
			return nil, errors.New("not found")
		},
	})

	_, err := svc.GetKey(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
