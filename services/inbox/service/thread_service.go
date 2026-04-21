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

	"agentmail/pkg/auth"
	dbpkg "agentmail/pkg/db"
	"agentmail/pkg/events"
	"agentmail/pkg/models"
	"agentmail/services/inbox/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ThreadService handles thread business logic.
type ThreadService struct {
	pool        *pgxpool.Pool
	threadStore store.ThreadStore
	inboxStore  store.InboxStore
	producer    events.EventPublisher
}

// NewThreadService creates a new ThreadService.
func NewThreadService(pool *pgxpool.Pool, threadStore store.ThreadStore, inboxStore store.InboxStore, producer events.EventPublisher) *ThreadService {
	return &ThreadService{
		pool:        pool,
		threadStore: threadStore,
		inboxStore:  inboxStore,
		producer:    producer,
	}
}

// List returns a paginated list of threads for an inbox.
func (s *ThreadService) List(ctx context.Context, claims *auth.Claims, q store.ThreadListQuery) ([]*models.Thread, string, error) {
	var threads []*models.Thread
	var nextCursor string
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		threads, nextCursor, err = s.threadStore.List(ctx, tx, claims.OrgID, q)
		return err
	})
	if err != nil {
		return nil, "", fmt.Errorf("list threads: %w", err)
	}
	return threads, nextCursor, nil
}

// Get retrieves a thread by ID.
func (s *ThreadService) Get(ctx context.Context, claims *auth.Claims, threadID uuid.UUID) (*models.Thread, error) {
	var thread *models.Thread
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		thread, err = s.threadStore.GetByID(ctx, tx, claims.OrgID, threadID)
		if err != nil {
			return err
		}
		labels, err := s.threadStore.GetLabels(ctx, tx, claims.OrgID, threadID)
		if err != nil {
			return err
		}
		for _, l := range labels {
			thread.Labels = append(thread.Labels, *l)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("get thread: %w", err)
	}
	return thread, nil
}

// UpdateStatus changes the status of a thread.
func (s *ThreadService) UpdateStatus(ctx context.Context, claims *auth.Claims, threadID uuid.UUID, status string) (*models.Thread, error) {
	var thread *models.Thread
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		existing, err := s.threadStore.GetByID(ctx, tx, claims.OrgID, threadID)
		if err != nil {
			return err
		}
		oldStatus := string(existing.Status)
		thread, err = s.threadStore.Update(ctx, tx, claims.OrgID, threadID, store.ThreadPatch{Status: &status})
		if err != nil {
			return err
		}
		evt := &events.ThreadStatusChangedEvent{
			BaseEvent: events.NewBase(events.EventThreadStatusChanged, claims.OrgID),
			Data: events.ThreadStatusChangedData{
				ThreadID:  threadID,
				InboxID:   thread.InboxID,
				OldStatus: oldStatus,
				NewStatus: status,
			},
		}
		_ = s.producer.PublishEvent(ctx, evt)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update thread status: %w", err)
	}
	return thread, nil
}

// MarkRead sets the is_read flag on a thread.
func (s *ThreadService) MarkRead(ctx context.Context, claims *auth.Claims, threadID uuid.UUID, isRead bool) (*models.Thread, error) {
	var thread *models.Thread
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		thread, err = s.threadStore.Update(ctx, tx, claims.OrgID, threadID, store.ThreadPatch{IsRead: &isRead})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("mark thread read: %w", err)
	}
	return thread, nil
}

// MarkStarred sets the is_starred flag on a thread.
func (s *ThreadService) MarkStarred(ctx context.Context, claims *auth.Claims, threadID uuid.UUID, isStarred bool) (*models.Thread, error) {
	var thread *models.Thread
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		thread, err = s.threadStore.Update(ctx, tx, claims.OrgID, threadID, store.ThreadPatch{IsStarred: &isStarred})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("mark thread starred: %w", err)
	}
	return thread, nil
}

// ApplyLabel applies a label to a thread.
func (s *ThreadService) ApplyLabel(ctx context.Context, claims *auth.Claims, threadID, labelID uuid.UUID) error {
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		// Verify thread belongs to org
		if _, err := s.threadStore.GetByID(ctx, tx, claims.OrgID, threadID); err != nil {
			return err
		}
		return s.threadStore.ApplyLabel(ctx, tx, threadID, labelID)
	})
	if err != nil {
		return fmt.Errorf("apply label: %w", err)
	}
	return nil
}

// RemoveLabel removes a label from a thread.
func (s *ThreadService) RemoveLabel(ctx context.Context, claims *auth.Claims, threadID, labelID uuid.UUID) error {
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		if _, err := s.threadStore.GetByID(ctx, tx, claims.OrgID, threadID); err != nil {
			return err
		}
		return s.threadStore.RemoveLabel(ctx, tx, threadID, labelID)
	})
	if err != nil {
		return fmt.Errorf("remove label: %w", err)
	}
	return nil
}
