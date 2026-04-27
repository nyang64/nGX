/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	authpkg "agentmail/pkg/auth"
	"agentmail/pkg/embedder"
	"agentmail/pkg/models"
	"agentmail/pkg/pagination"
)

var (
	pool    *pgxpool.Pool
	embeddr *embedder.Client
)

func init() {
	pool = shared.InitDB()
	if u := os.Getenv("EMBEDDER_URL"); u != "" {
		dims := 0
		if s := os.Getenv("EMBEDDER_DIMS"); s != "" {
			if d, err := strconv.Atoi(s); err == nil && d > 0 {
				dims = d
			}
		}
		embeddr = embedder.New(u, os.Getenv("EMBEDDER_MODEL"), os.Getenv("EMBEDDER_API_KEY"), dims)
	}
}

type searchResult struct {
	MessageID  uuid.UUID           `json:"message_id"`
	ThreadID   uuid.UUID           `json:"thread_id"`
	InboxID    uuid.UUID           `json:"inbox_id"`
	Subject    string              `json:"subject"`
	Snippet    string              `json:"snippet"`
	From       models.EmailAddress `json:"from"`
	ReceivedAt *time.Time          `json:"received_at,omitempty"`
	SentAt     *time.Time          `json:"sent_at,omitempty"`
	Direction  models.Direction    `json:"direction"`
	Rank       float64             `json:"rank"`
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	claims, err := shared.ExtractClaims(event)
	if err != nil {
		return shared.Error(401, "unauthorized"), nil
	}

	if !claims.HasScope(authpkg.ScopeSearchRead) {
		return shared.Error(403, "insufficient scope"), nil
	}

	q := event.QueryStringParameters["q"]
	if q == "" {
		return shared.Error(400, "q parameter is required"), nil
	}

	limit := 20
	if s := event.QueryStringParameters["limit"]; s != "" {
		if v, e := strconv.Atoi(s); e == nil && v > 0 {
			limit = v
		}
	}
	if limit > 100 {
		limit = 100
	}

	var inboxID *uuid.UUID
	if s := event.QueryStringParameters["inbox_id"]; s != "" {
		id, e := uuid.Parse(s)
		if e != nil {
			return shared.Error(400, "invalid inbox_id"), nil
		}
		inboxID = &id
	}

	cursor := event.QueryStringParameters["cursor"]
	mode := event.QueryStringParameters["mode"]

	var (
		results    []searchResult
		nextCursor string
	)

	if mode == "semantic" && embeddr != nil {
		results, nextCursor, err = semanticSearch(ctx, claims.OrgID, q, inboxID, limit, cursor)
		if err != nil {
			results, nextCursor, err = keywordSearch(ctx, claims.OrgID, q, inboxID, limit, cursor)
		}
	} else {
		results, nextCursor, err = keywordSearch(ctx, claims.OrgID, q, inboxID, limit, cursor)
	}
	if err != nil {
		return shared.Error(500, "search failed"), nil
	}
	if results == nil {
		results = []searchResult{}
	}

	return shared.JSON(200, map[string]any{
		"items":       results,
		"next_cursor": nextCursor,
		"has_more":    nextCursor != "",
	}), nil
}

func keywordSearch(ctx context.Context, orgID uuid.UUID, query string, inboxID *uuid.UUID, limit int, cursor string) ([]searchResult, string, error) {
	args := []any{query, orgID}
	argIdx := 3

	inboxFilter := "NULL::uuid"
	if inboxID != nil {
		inboxFilter = fmt.Sprintf("$%d", argIdx)
		args = append(args, *inboxID)
		argIdx++
	}

	cursorFilter := ""
	if cursor != "" {
		parts, err := pagination.DecodeCursor(cursor)
		if err != nil || len(parts) < 2 {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		cursorFilter = fmt.Sprintf("AND (COALESCE(m.received_at, m.sent_at, m.created_at), m.id) < ($%d::timestamptz, $%d::uuid)", argIdx, argIdx+1)
		args = append(args, parts[0], parts[1])
		argIdx += 2
	}

	args = append(args, limit+1)

	sql := fmt.Sprintf(`
		SELECT m.id, m.thread_id, m.inbox_id, m.subject,
		       COALESCE(t.snippet, ''), m.from_address, m.from_name,
		       m.received_at, m.sent_at, m.direction,
		       ts_rank(m.search_vector, plainto_tsquery('english', $1)) AS rank
		FROM messages m
		LEFT JOIN threads t ON t.id = m.thread_id
		WHERE m.org_id = $2
		  AND m.search_vector @@ plainto_tsquery('english', $1)
		  AND (%s::uuid IS NULL OR m.inbox_id = %s::uuid)
		  %s
		ORDER BY rank DESC, COALESCE(m.received_at, m.sent_at, m.created_at) DESC, m.id DESC
		LIMIT $%d`, inboxFilter, inboxFilter, cursorFilter, argIdx)

	return runSearch(ctx, sql, args, limit)
}

func semanticSearch(ctx context.Context, orgID uuid.UUID, query string, inboxID *uuid.UUID, limit int, cursor string) ([]searchResult, string, error) {
	vec, err := embeddr.Embed(ctx, query)
	if err != nil {
		return nil, "", err
	}
	vecLit := embedder.VectorLiteral(vec)

	args := []any{vecLit, orgID}
	argIdx := 3

	inboxFilter := "NULL::uuid"
	if inboxID != nil {
		inboxFilter = fmt.Sprintf("$%d", argIdx)
		args = append(args, *inboxID)
		argIdx++
	}

	cursorFilter := ""
	if cursor != "" {
		parts, err := pagination.DecodeCursor(cursor)
		if err != nil || len(parts) < 2 {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		cursorFilter = fmt.Sprintf("AND (COALESCE(m.received_at, m.sent_at, m.created_at), m.id) < ($%d::timestamptz, $%d::uuid)", argIdx, argIdx+1)
		args = append(args, parts[0], parts[1])
		argIdx += 2
	}

	args = append(args, limit+1)

	sql := fmt.Sprintf(`
		SELECT m.id, m.thread_id, m.inbox_id, m.subject,
		       COALESCE(t.snippet, ''), m.from_address, m.from_name,
		       m.received_at, m.sent_at, m.direction,
		       1 - (m.embedding <=> $1::vector) AS rank
		FROM messages m
		LEFT JOIN threads t ON t.id = m.thread_id
		WHERE m.org_id = $2
		  AND m.embedding IS NOT NULL
		  AND (%s::uuid IS NULL OR m.inbox_id = %s::uuid)
		  %s
		ORDER BY m.embedding <=> $1::vector ASC, COALESCE(m.received_at, m.sent_at, m.created_at) DESC, m.id DESC
		LIMIT $%d`, inboxFilter, inboxFilter, cursorFilter, argIdx)

	return runSearch(ctx, sql, args, limit)
}

func runSearch(ctx context.Context, sql string, args []any, limit int) ([]searchResult, string, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var results []searchResult
	for rows.Next() {
		var sr searchResult
		var fromEmail, fromName string
		if err := rows.Scan(
			&sr.MessageID, &sr.ThreadID, &sr.InboxID, &sr.Subject, &sr.Snippet,
			&fromEmail, &fromName, &sr.ReceivedAt, &sr.SentAt, &sr.Direction, &sr.Rank,
		); err != nil {
			return nil, "", err
		}
		sr.From = models.EmailAddress{Email: fromEmail, Name: fromName}
		results = append(results, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(results) > limit {
		results = results[:limit]
		last := results[len(results)-1]
		cursorTs := last.ReceivedAt
		if cursorTs == nil {
			cursorTs = last.SentAt
		}
		var tsStr string
		if cursorTs != nil {
			tsStr = cursorTs.Format(time.RFC3339Nano)
		}
		nextCursor = pagination.EncodeCursor(tsStr, last.MessageID.String())
	}
	return results, nextCursor, nil
}

func main() {
	lambda.Start(handler)
}
