package service

import (
	"context"
	"fmt"
	"time"

	"agentmail/pkg/auth"
	dbpkg "agentmail/pkg/db"
	"agentmail/pkg/events"
	"agentmail/pkg/kafka"
	"agentmail/pkg/models"
	"agentmail/services/inbox/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateInboxRequest is the input for creating a new inbox.
type CreateInboxRequest struct {
	PodID       *uuid.UUID
	Address     string
	DisplayName string
	Settings    map[string]any
}

// UpdateInboxRequest is the input for updating an inbox.
type UpdateInboxRequest struct {
	DisplayName *string
	Status      *string
	Settings    map[string]any
}

// InboxService handles inbox business logic.
type InboxService struct {
	pool       *pgxpool.Pool
	inboxStore store.InboxStore
	producer   *kafka.Producer
}

// NewInboxService creates a new InboxService.
func NewInboxService(pool *pgxpool.Pool, inboxStore store.InboxStore, producer *kafka.Producer) *InboxService {
	return &InboxService{pool: pool, inboxStore: inboxStore, producer: producer}
}

// Create provisions a new inbox.
func (s *InboxService) Create(ctx context.Context, claims *auth.Claims, req CreateInboxRequest) (*models.Inbox, error) {
	// For pod-scoped keys, default to and enforce the key's pod.
	podID := req.PodID
	if claims.PodID != nil {
		if podID == nil {
			podID = claims.PodID
		} else if *podID != *claims.PodID {
			return nil, fmt.Errorf("pod-scoped key cannot create inboxes in a different pod")
		}
	}

	inbox := &models.Inbox{
		ID:          uuid.New(),
		OrgID:       claims.OrgID,
		PodID:       podID,
		Email:       req.Address,
		DisplayName: req.DisplayName,
		Status:      models.InboxStatusActive,
		Settings:    req.Settings,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if inbox.Settings == nil {
		inbox.Settings = map[string]any{}
	}

	err := dbpkg.WithOrgPodTx(ctx, s.pool, claims.OrgID, claims.PodID, func(tx pgx.Tx) error {
		return s.inboxStore.Create(ctx, tx, inbox)
	})
	if err != nil {
		return nil, fmt.Errorf("create inbox: %w", err)
	}

	evtPodID := uuid.Nil
	if inbox.PodID != nil {
		evtPodID = *inbox.PodID
	}
	evt := &events.InboxCreatedEvent{
		BaseEvent: events.NewBase(events.EventInboxCreated, claims.OrgID),
		Data: events.InboxCreatedData{
			InboxID:      inbox.ID,
			EmailAddress: inbox.Email,
			PodID:        evtPodID,
		},
	}
	_ = s.producer.PublishEvent(ctx, evt) // best-effort

	return inbox, nil
}

// Get retrieves an inbox by ID.
func (s *InboxService) Get(ctx context.Context, claims *auth.Claims, inboxID uuid.UUID) (*models.Inbox, error) {
	var inbox *models.Inbox
	err := dbpkg.WithOrgPodTx(ctx, s.pool, claims.OrgID, claims.PodID, func(tx pgx.Tx) error {
		var err error
		inbox, err = s.inboxStore.GetByID(ctx, tx, claims.OrgID, inboxID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("get inbox: %w", err)
	}
	return inbox, nil
}

// List returns a paginated list of inboxes.
func (s *InboxService) List(ctx context.Context, claims *auth.Claims, podID *uuid.UUID, limit int, cursor string) ([]*models.Inbox, string, error) {
	// For pod-scoped keys, restrict to the key's pod regardless of the query param.
	if claims.PodID != nil {
		podID = claims.PodID
	}
	var inboxes []*models.Inbox
	var nextCursor string
	err := dbpkg.WithOrgPodTx(ctx, s.pool, claims.OrgID, claims.PodID, func(tx pgx.Tx) error {
		var err error
		inboxes, nextCursor, err = s.inboxStore.List(ctx, tx, claims.OrgID, podID, limit, cursor)
		return err
	})
	if err != nil {
		return nil, "", fmt.Errorf("list inboxes: %w", err)
	}
	return inboxes, nextCursor, nil
}

// Update modifies an inbox.
func (s *InboxService) Update(ctx context.Context, claims *auth.Claims, inboxID uuid.UUID, req UpdateInboxRequest) (*models.Inbox, error) {
	patch := store.InboxPatch{
		DisplayName: req.DisplayName,
		Status:      req.Status,
		Settings:    req.Settings,
	}
	var inbox *models.Inbox
	err := dbpkg.WithOrgPodTx(ctx, s.pool, claims.OrgID, claims.PodID, func(tx pgx.Tx) error {
		var err error
		inbox, err = s.inboxStore.Update(ctx, tx, claims.OrgID, inboxID, patch)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("update inbox: %w", err)
	}
	return inbox, nil
}

// Delete removes an inbox.
func (s *InboxService) Delete(ctx context.Context, claims *auth.Claims, inboxID uuid.UUID) error {
	err := dbpkg.WithOrgPodTx(ctx, s.pool, claims.OrgID, claims.PodID, func(tx pgx.Tx) error {
		return s.inboxStore.Delete(ctx, tx, claims.OrgID, inboxID)
	})
	if err != nil {
		return fmt.Errorf("delete inbox: %w", err)
	}
	return nil
}
