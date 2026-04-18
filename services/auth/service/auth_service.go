/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	authpkg "agentmail/pkg/auth"
	"agentmail/pkg/models"
	"agentmail/services/auth/store"
)

// CreateKeyRequest holds the parameters for creating a new API key.
type CreateKeyRequest struct {
	OrgID  uuid.UUID
	Name   string
	Scopes []string
	PodID  *uuid.UUID
}

// AuthService provides API key management operations.
type AuthService struct {
	store store.APIKeyStore
}

// NewAuthService creates a new AuthService backed by the given store.
func NewAuthService(s store.APIKeyStore) *AuthService {
	return &AuthService{store: s}
}

// CreateKey validates the request and delegates to the store to create a new API key.
// It returns the key model and the one-time plaintext key string.
func (s *AuthService) CreateKey(ctx context.Context, req CreateKeyRequest) (*models.APIKey, string, error) {
	if len(req.Scopes) == 0 {
		return nil, "", fmt.Errorf("at least one scope required")
	}
	return s.store.Create(ctx, req.OrgID, req.Name, req.Scopes, req.PodID)
}

// ValidateKey validates a plaintext API key and returns the corresponding Claims.
func (s *AuthService) ValidateKey(ctx context.Context, plaintextKey string) (*authpkg.Claims, error) {
	key, err := s.store.Validate(ctx, plaintextKey)
	if err != nil {
		return nil, fmt.Errorf("invalid key")
	}

	scopes := make([]authpkg.Scope, len(key.Scopes))
	for i, sc := range key.Scopes {
		scopes[i] = authpkg.Scope(sc)
	}

	return &authpkg.Claims{
		OrgID:  key.OrgID,
		KeyID:  key.ID,
		Scopes: scopes,
		PodID:  key.PodID,
	}, nil
}

// RevokeKey revokes an API key belonging to the given org.
func (s *AuthService) RevokeKey(ctx context.Context, orgID, keyID uuid.UUID) error {
	return s.store.Revoke(ctx, orgID, keyID)
}

// ListKeys returns all active API keys for the given org.
func (s *AuthService) ListKeys(ctx context.Context, orgID uuid.UUID) ([]*models.APIKey, error) {
	return s.store.List(ctx, orgID)
}

// GetKey retrieves a single API key by ID, scoped to the given org.
func (s *AuthService) GetKey(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, error) {
	return s.store.GetByID(ctx, orgID, keyID)
}
