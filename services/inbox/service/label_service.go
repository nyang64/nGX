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
	"time"

	"agentmail/pkg/auth"
	dbpkg "agentmail/pkg/db"
	"agentmail/pkg/models"
	"agentmail/services/inbox/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateLabelRequest is the input for creating a label.
type CreateLabelRequest struct {
	Name        string
	Color       string
	Description string
}

// UpdateLabelRequest is the input for updating a label.
type UpdateLabelRequest struct {
	Name        *string
	Color       *string
	Description *string
}

// LabelService handles label business logic.
type LabelService struct {
	pool       *pgxpool.Pool
	labelStore store.LabelStore
}

// NewLabelService creates a new LabelService.
func NewLabelService(pool *pgxpool.Pool, labelStore store.LabelStore) *LabelService {
	return &LabelService{pool: pool, labelStore: labelStore}
}

// Create stores a new label.
func (s *LabelService) Create(ctx context.Context, claims *auth.Claims, req CreateLabelRequest) (*models.Label, error) {
	label := &models.Label{
		ID:          uuid.New(),
		OrgID:       claims.OrgID,
		Name:        req.Name,
		Color:       req.Color,
		Description: req.Description,
		CreatedAt:   time.Now().UTC(),
	}

	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		return s.labelStore.Create(ctx, tx, label)
	})
	if err != nil {
		return nil, fmt.Errorf("create label: %w", err)
	}
	return label, nil
}

// Get retrieves a label by ID.
func (s *LabelService) Get(ctx context.Context, claims *auth.Claims, labelID uuid.UUID) (*models.Label, error) {
	var label *models.Label
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		label, err = s.labelStore.GetByID(ctx, tx, claims.OrgID, labelID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("get label: %w", err)
	}
	return label, nil
}

// List returns all labels for an org.
func (s *LabelService) List(ctx context.Context, claims *auth.Claims) ([]*models.Label, error) {
	var labels []*models.Label
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		labels, err = s.labelStore.List(ctx, tx, claims.OrgID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	return labels, nil
}

// Update modifies a label.
func (s *LabelService) Update(ctx context.Context, claims *auth.Claims, labelID uuid.UUID, req UpdateLabelRequest) (*models.Label, error) {
	var label *models.Label
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		label, err = s.labelStore.Update(ctx, tx, claims.OrgID, labelID, store.LabelPatch{
			Name:        req.Name,
			Color:       req.Color,
			Description: req.Description,
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("update label: %w", err)
	}
	return label, nil
}

// Delete removes a label.
func (s *LabelService) Delete(ctx context.Context, claims *auth.Claims, labelID uuid.UUID) error {
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		return s.labelStore.Delete(ctx, tx, claims.OrgID, labelID)
	})
	if err != nil {
		return fmt.Errorf("delete label: %w", err)
	}
	return nil
}
