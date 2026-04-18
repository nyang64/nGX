/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package store

import (
	"context"
	"fmt"

	"agentmail/pkg/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WebhookSubscriptionStore provides read access to webhook subscriptions.
type WebhookSubscriptionStore struct {
	pool *pgxpool.Pool
}

// NewWebhookSubscriptionStore creates a new WebhookSubscriptionStore.
func NewWebhookSubscriptionStore(pool *pgxpool.Pool) *WebhookSubscriptionStore {
	return &WebhookSubscriptionStore{pool: pool}
}

// FindMatchingWebhooks returns active webhooks for an org that subscribe to the given event type.
// A webhook matches if its events array contains either the specific event type or the wildcard "*".
func (s *WebhookSubscriptionStore) FindMatchingWebhooks(ctx context.Context, orgID uuid.UUID, eventType string) ([]*models.Webhook, error) {
	q := `
		SELECT id, org_id, url, secret, events, pod_id, inbox_id,
		       is_active, failure_count, last_success_at, last_failure_at,
		       created_at, updated_at
		FROM webhooks
		WHERE org_id = $1
		  AND is_active = true
		  AND (events @> ARRAY[$2]::text[] OR events @> ARRAY['*']::text[])
	`
	rows, err := s.pool.Query(ctx, q, orgID, eventType)
	if err != nil {
		return nil, fmt.Errorf("query matching webhooks: %w", err)
	}
	defer rows.Close()

	var webhooks []*models.Webhook
	for rows.Next() {
		var wh models.Webhook
		err := rows.Scan(
			&wh.ID,
			&wh.OrgID,
			&wh.URL,
			&wh.Secret,
			&wh.Events,
			&wh.PodID,
			&wh.InboxID,
			&wh.IsActive,
			&wh.FailureCount,
			&wh.LastSuccessAt,
			&wh.LastFailureAt,
			&wh.CreatedAt,
			&wh.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}
		webhooks = append(webhooks, &wh)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhooks: %w", err)
	}
	return webhooks, nil
}
