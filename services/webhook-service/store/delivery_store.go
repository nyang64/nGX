package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agentmail/pkg/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeliveryStore manages webhook delivery records and webhook lookups.
type DeliveryStore struct {
	pool *pgxpool.Pool
}

// NewDeliveryStore creates a new DeliveryStore.
func NewDeliveryStore(pool *pgxpool.Pool) *DeliveryStore {
	return &DeliveryStore{pool: pool}
}

// GetWebhook retrieves a webhook by ID (without RLS, for internal use).
func (s *DeliveryStore) GetWebhook(ctx context.Context, webhookID uuid.UUID) (*models.Webhook, error) {
	q := `
		SELECT id, org_id, url, secret, events, pod_id, inbox_id,
		       is_active, failure_count, last_success_at, last_failure_at,
		       created_at, updated_at,
		       auth_header_name, auth_header_value_enc
		FROM webhooks
		WHERE id = $1
	`
	row := s.pool.QueryRow(ctx, q, webhookID)
	var wh models.Webhook
	err := row.Scan(
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
		&wh.AuthHeaderName,
		&wh.AuthHeaderValueEnc,
	)
	if err != nil {
		return nil, fmt.Errorf("get webhook %s: %w", webhookID, err)
	}
	return &wh, nil
}

// ListWebhooks returns all webhooks for the given org (no RLS).
func (s *DeliveryStore) ListWebhooks(ctx context.Context, orgID uuid.UUID) ([]*models.Webhook, error) {
	q := `
		SELECT id, org_id, url, secret, events, pod_id, inbox_id,
		       is_active, failure_count, last_success_at, last_failure_at,
		       created_at, updated_at,
		       auth_header_name, auth_header_value_enc
		FROM webhooks
		WHERE org_id = $1
		ORDER BY created_at DESC
	`
	rows, err := s.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("list webhooks for org %s: %w", orgID, err)
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
			&wh.AuthHeaderName,
			&wh.AuthHeaderValueEnc,
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

// CreateWebhook inserts a new webhook record.
func (s *DeliveryStore) CreateWebhook(ctx context.Context, wh *models.Webhook) error {
	q := `
		INSERT INTO webhooks
			(id, org_id, url, secret, events, pod_id, inbox_id,
			 is_active, failure_count, auth_header_name, auth_header_value_enc,
			 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0, $9, $10, NOW(), NOW())
		RETURNING created_at, updated_at
	`
	row := s.pool.QueryRow(ctx, q,
		wh.ID,
		wh.OrgID,
		wh.URL,
		wh.Secret,
		wh.Events,
		wh.PodID,
		wh.InboxID,
		wh.IsActive,
		wh.AuthHeaderName,
		wh.AuthHeaderValueEnc,
	)
	return row.Scan(&wh.CreatedAt, &wh.UpdatedAt)
}

// GetWebhookByIDAndOrg retrieves a webhook by ID filtered by org_id (RLS-like check).
func (s *DeliveryStore) GetWebhookByIDAndOrg(ctx context.Context, id, orgID uuid.UUID) (*models.Webhook, error) {
	q := `
		SELECT id, org_id, url, secret, events, pod_id, inbox_id,
		       is_active, failure_count, last_success_at, last_failure_at,
		       created_at, updated_at,
		       auth_header_name, auth_header_value_enc
		FROM webhooks
		WHERE id = $1 AND org_id = $2
	`
	row := s.pool.QueryRow(ctx, q, id, orgID)
	var wh models.Webhook
	err := row.Scan(
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
		&wh.AuthHeaderName,
		&wh.AuthHeaderValueEnc,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("get webhook %s: %w", id, err)
	}
	return &wh, nil
}

// UpdateWebhook updates mutable fields of a webhook, scoped by org_id.
func (s *DeliveryStore) UpdateWebhook(ctx context.Context, wh *models.Webhook) error {
	q := `
		UPDATE webhooks
		SET url                   = $1,
		    events                = $2,
		    is_active             = $3,
		    auth_header_name      = $4,
		    auth_header_value_enc = $5,
		    updated_at            = NOW()
		WHERE id = $6 AND org_id = $7
		RETURNING updated_at
	`
	row := s.pool.QueryRow(ctx, q,
		wh.URL,
		wh.Events,
		wh.IsActive,
		wh.AuthHeaderName,
		wh.AuthHeaderValueEnc,
		wh.ID,
		wh.OrgID,
	)
	return row.Scan(&wh.UpdatedAt)
}

// DeleteWebhook deletes a webhook, scoped by org_id.
func (s *DeliveryStore) DeleteWebhook(ctx context.Context, id, orgID uuid.UUID) error {
	q := `DELETE FROM webhooks WHERE id = $1 AND org_id = $2`
	tag, err := s.pool.Exec(ctx, q, id, orgID)
	if err != nil {
		return fmt.Errorf("delete webhook %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ListDeliveries returns delivery records for a given webhook, scoped by org via join.
func (s *DeliveryStore) ListDeliveries(ctx context.Context, webhookID, orgID uuid.UUID) ([]*models.WebhookDelivery, error) {
	q := `
		SELECT wd.id, wd.webhook_id, wd.event_id, wd.event_type, wd.payload, wd.status,
		       wd.attempt_count, wd.next_attempt_at, wd.last_attempt_at,
		       wd.response_status, wd.response_body, wd.error_message,
		       wd.created_at, wd.updated_at
		FROM webhook_deliveries wd
		JOIN webhooks w ON w.id = wd.webhook_id
		WHERE wd.webhook_id = $1 AND w.org_id = $2
		ORDER BY wd.created_at DESC
		LIMIT 100
	`
	rows, err := s.pool.Query(ctx, q, webhookID, orgID)
	if err != nil {
		return nil, fmt.Errorf("list deliveries for webhook %s: %w", webhookID, err)
	}
	defer rows.Close()

	var deliveries []*models.WebhookDelivery
	for rows.Next() {
		var d models.WebhookDelivery
		err := rows.Scan(
			&d.ID,
			&d.WebhookID,
			&d.EventID,
			&d.EventType,
			&d.Payload,
			&d.Status,
			&d.AttemptCount,
			&d.NextAttemptAt,
			&d.LastAttemptAt,
			&d.ResponseStatus,
			&d.ResponseBody,
			&d.ErrorMessage,
			&d.CreatedAt,
			&d.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		deliveries = append(deliveries, &d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deliveries: %w", err)
	}
	return deliveries, nil
}

// CreateDelivery inserts a new webhook delivery record with status=pending.
func (s *DeliveryStore) CreateDelivery(ctx context.Context, d *models.WebhookDelivery) error {
	q := `
		INSERT INTO webhook_deliveries
			(id, webhook_id, event_id, event_type, payload, status,
			 attempt_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
	`
	_, err := s.pool.Exec(ctx, q,
		d.ID,
		d.WebhookID,
		d.EventID,
		d.EventType,
		d.Payload,
		string(d.Status),
		d.AttemptCount,
	)
	if err != nil {
		return fmt.Errorf("create webhook delivery: %w", err)
	}
	return nil
}

// MarkSuccess updates the delivery record to status=success.
func (s *DeliveryStore) MarkSuccess(ctx context.Context, d *models.WebhookDelivery) error {
	q := `
		UPDATE webhook_deliveries
		SET status          = 'success',
		    attempt_count   = $1,
		    last_attempt_at = $2,
		    response_status = $3,
		    response_body   = $4,
		    next_attempt_at = NULL,
		    updated_at      = NOW()
		WHERE id = $5
	`
	_, err := s.pool.Exec(ctx, q,
		d.AttemptCount,
		d.LastAttemptAt,
		d.ResponseStatus,
		d.ResponseBody,
		d.ID,
	)
	if err != nil {
		return fmt.Errorf("mark delivery success: %w", err)
	}

	// Update the webhook's last_success_at.
	_, _ = s.pool.Exec(ctx,
		`UPDATE webhooks SET last_success_at = NOW(), failure_count = 0, updated_at = NOW() WHERE id = $1`,
		d.WebhookID,
	)
	return nil
}

// MarkFailed increments the attempt count and schedules a retry (or sets
// status=failed if maxRetries is exceeded based on the attempt count).
// The caller is responsible for deciding whether this is a final failure.
func (s *DeliveryStore) MarkFailed(ctx context.Context, d *models.WebhookDelivery, attempt int) error {
	backoff := nextBackoff(attempt)
	nextAttempt := time.Now().Add(backoff)

	status := models.DeliveryStatusRetrying
	if attempt >= maxRetries(d) {
		status = models.DeliveryStatusFailed
		nextAttempt = time.Time{} // no next attempt
	}

	var nextAttemptPtr *time.Time
	if status == models.DeliveryStatusRetrying {
		nextAttemptPtr = &nextAttempt
	}

	q := `
		UPDATE webhook_deliveries
		SET status          = $1,
		    attempt_count   = $2,
		    last_attempt_at = $3,
		    next_attempt_at = $4,
		    response_status = $5,
		    response_body   = $6,
		    error_message   = $7,
		    updated_at      = NOW()
		WHERE id = $8
	`
	_, err := s.pool.Exec(ctx, q,
		string(status),
		attempt,
		d.LastAttemptAt,
		nextAttemptPtr,
		d.ResponseStatus,
		d.ResponseBody,
		d.ErrorMessage,
		d.ID,
	)
	if err != nil {
		return fmt.Errorf("mark delivery failed: %w", err)
	}

	// Update the webhook's failure tracking.
	_, _ = s.pool.Exec(ctx,
		`UPDATE webhooks SET last_failure_at = NOW(), failure_count = failure_count + 1, updated_at = NOW() WHERE id = $1`,
		d.WebhookID,
	)
	return nil
}

// GetPendingRetries returns deliveries with status='retrying' whose next_attempt_at is due.
func (s *DeliveryStore) GetPendingRetries(ctx context.Context) ([]*models.WebhookDelivery, error) {
	q := `
		SELECT id, webhook_id, event_id, event_type, payload, status,
		       attempt_count, next_attempt_at, last_attempt_at,
		       response_status, response_body, error_message,
		       created_at, updated_at
		FROM webhook_deliveries
		WHERE status = 'retrying'
		  AND next_attempt_at <= NOW()
		ORDER BY next_attempt_at ASC
		LIMIT 100
	`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query pending retries: %w", err)
	}
	defer rows.Close()

	var deliveries []*models.WebhookDelivery
	for rows.Next() {
		var d models.WebhookDelivery
		err := rows.Scan(
			&d.ID,
			&d.WebhookID,
			&d.EventID,
			&d.EventType,
			&d.Payload,
			&d.Status,
			&d.AttemptCount,
			&d.NextAttemptAt,
			&d.LastAttemptAt,
			&d.ResponseStatus,
			&d.ResponseBody,
			&d.ErrorMessage,
			&d.CreatedAt,
			&d.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		deliveries = append(deliveries, &d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deliveries: %w", err)
	}
	return deliveries, nil
}

// nextBackoff mirrors the delivery package's backoff logic (2^attempt seconds, capped at 1h).
func nextBackoff(attempt int) time.Duration {
	if attempt > 12 {
		attempt = 12
	}
	return time.Duration(1<<uint(attempt)) * time.Second
}

// maxRetries returns the hard-coded retry ceiling used by the store.
// The RetryScheduler also enforces this from config; the store uses 8 as a default.
func maxRetries(_ *models.WebhookDelivery) int {
	return 8
}
