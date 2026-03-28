package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	authpkg "agentmail/pkg/auth"
	"agentmail/pkg/embedder"
	"agentmail/pkg/models"
	"agentmail/pkg/pagination"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SearchResult is a single message returned by a search query.
type SearchResult struct {
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

// SearchResponse is the paginated response body.
type SearchResponse struct {
	Items      []SearchResult `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
}

// SearchHandler handles GET /search.
type SearchHandler struct {
	pool    *pgxpool.Pool
	embeddr *embedder.Client // nil when semantic search is not configured
}

// NewSearchHandler creates a SearchHandler.
// embeddr may be nil; semantic search is silently disabled when it is.
func NewSearchHandler(pool *pgxpool.Pool, embeddr *embedder.Client) *SearchHandler {
	return &SearchHandler{pool: pool, embeddr: embeddr}
}

// Search handles GET /search?q=...&inbox_id=...&limit=...&cursor=...&mode=...
//
// mode=keyword (default): PostgreSQL full-text search via tsvector/tsquery.
// mode=semantic:          cosine nearest-neighbour search on message embeddings.
//                         Falls back to keyword search when the embedder is not
//                         configured or the query cannot be embedded.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	orgID := authpkg.OrgIDFromCtx(r.Context())
	if orgID == uuid.Nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil {
			limit = v
		}
	}
	limit = pagination.ClampLimit(limit)

	var inboxID *uuid.UUID
	if inboxIDStr := r.URL.Query().Get("inbox_id"); inboxIDStr != "" {
		id, err := uuid.Parse(inboxIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid inbox_id")
			return
		}
		inboxID = &id
	}

	cursor := r.URL.Query().Get("cursor")
	mode := r.URL.Query().Get("mode")

	var (
		results    []SearchResult
		nextCursor string
		err        error
	)

	if mode == "semantic" && h.embeddr != nil {
		results, nextCursor, err = h.semanticSearch(r.Context(), orgID, q, inboxID, limit, cursor)
		if err != nil {
			// Degrade gracefully to keyword search on embedder failure.
			results, nextCursor, err = h.keywordSearch(r.Context(), orgID, q, inboxID, limit, cursor)
		}
	} else {
		results, nextCursor, err = h.keywordSearch(r.Context(), orgID, q, inboxID, limit, cursor)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	writeJSON(w, http.StatusOK, SearchResponse{
		Items:      results,
		NextCursor: nextCursor,
		HasMore:    nextCursor != "",
	})
}

func (h *SearchHandler) keywordSearch(
	ctx context.Context,
	orgID uuid.UUID,
	query string,
	inboxID *uuid.UUID,
	limit int,
	cursor string,
) ([]SearchResult, string, error) {
	// Build the WHERE clause. We always filter by org_id and the tsquery match.
	// An optional inbox_id filter and cursor are appended dynamically.
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
		// Cursor encodes (rank_str, id) — we approximate with received_at + id keyset.
		cursorFilter = fmt.Sprintf(
			"AND (m.received_at, m.id) < ($%d::timestamptz, $%d::uuid)",
			argIdx, argIdx+1,
		)
		args = append(args, parts[0], parts[1])
		argIdx += 2
	}

	args = append(args, limit+1)

	// NOTE: messages do not have a dedicated snippet column; the thread snippet
	// is joined from the threads table to provide a preview.
	sql := fmt.Sprintf(`
		SELECT
			m.id,
			m.thread_id,
			m.inbox_id,
			m.subject,
			COALESCE(t.snippet, ''),
			m.from_address,
			m.from_name,
			m.received_at,
			m.sent_at,
			m.direction,
			ts_rank(m.search_vector, plainto_tsquery('english', $1)) AS rank
		FROM messages m
		LEFT JOIN threads t ON t.id = m.thread_id
		WHERE m.org_id = $2
		  AND m.search_vector @@ plainto_tsquery('english', $1)
		  AND (%s::uuid IS NULL OR m.inbox_id = %s::uuid)
		  %s
		ORDER BY rank DESC, m.received_at DESC
		LIMIT $%d
	`, inboxFilter, inboxFilter, cursorFilter, argIdx)

	rows, err := h.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var sr SearchResult
		var fromEmail, fromName string
		err := rows.Scan(
			&sr.MessageID,
			&sr.ThreadID,
			&sr.InboxID,
			&sr.Subject,
			&sr.Snippet,
			&fromEmail,
			&fromName,
			&sr.ReceivedAt,
			&sr.SentAt,
			&sr.Direction,
			&sr.Rank,
		)
		if err != nil {
			return nil, "", fmt.Errorf("scan search result: %w", err)
		}
		sr.From = models.EmailAddress{Email: fromEmail, Name: fromName}
		results = append(results, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate search results: %w", err)
	}

	var nextCursor string
	if len(results) > limit {
		results = results[:limit]
		last := results[len(results)-1]
		var receivedAt string
		if last.ReceivedAt != nil {
			receivedAt = last.ReceivedAt.Format(time.RFC3339Nano)
		}
		nextCursor = pagination.EncodeCursor(receivedAt, last.MessageID.String())
	}

	return results, nextCursor, nil
}

// semanticSearch embeds the query and finds nearest-neighbour messages by
// cosine similarity on the messages.embedding column.
func (h *SearchHandler) semanticSearch(
	ctx context.Context,
	orgID uuid.UUID,
	query string,
	inboxID *uuid.UUID,
	limit int,
	cursor string,
) ([]SearchResult, string, error) {
	vec, err := h.embeddr.Embed(ctx, query)
	if err != nil {
		return nil, "", fmt.Errorf("embed query: %w", err)
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

	// Cursor encodes (similarity_str, id); we approximate with received_at + id.
	cursorFilter := ""
	if cursor != "" {
		parts, err := pagination.DecodeCursor(cursor)
		if err != nil || len(parts) < 2 {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		cursorFilter = fmt.Sprintf(
			"AND (m.received_at, m.id) < ($%d::timestamptz, $%d::uuid)",
			argIdx, argIdx+1,
		)
		args = append(args, parts[0], parts[1])
		argIdx += 2
	}

	args = append(args, limit+1)

	sql := fmt.Sprintf(`
		SELECT
			m.id,
			m.thread_id,
			m.inbox_id,
			m.subject,
			COALESCE(t.snippet, ''),
			m.from_address,
			m.from_name,
			m.received_at,
			m.sent_at,
			m.direction,
			1 - (m.embedding <=> $1::vector) AS rank
		FROM messages m
		LEFT JOIN threads t ON t.id = m.thread_id
		WHERE m.org_id = $2
		  AND m.embedding IS NOT NULL
		  AND (%s::uuid IS NULL OR m.inbox_id = %s::uuid)
		  %s
		ORDER BY m.embedding <=> $1::vector ASC
		LIMIT $%d
	`, inboxFilter, inboxFilter, cursorFilter, argIdx)

	rows, err := h.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", fmt.Errorf("semantic search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var sr SearchResult
		var fromEmail, fromName string
		if err := rows.Scan(
			&sr.MessageID, &sr.ThreadID, &sr.InboxID,
			&sr.Subject, &sr.Snippet,
			&fromEmail, &fromName,
			&sr.ReceivedAt, &sr.SentAt, &sr.Direction,
			&sr.Rank,
		); err != nil {
			return nil, "", fmt.Errorf("scan semantic result: %w", err)
		}
		sr.From = models.EmailAddress{Email: fromEmail, Name: fromName}
		results = append(results, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate semantic results: %w", err)
	}

	var nextCursor string
	if len(results) > limit {
		results = results[:limit]
		last := results[len(results)-1]
		var receivedAt string
		if last.ReceivedAt != nil {
			receivedAt = last.ReceivedAt.Format(time.RFC3339Nano)
		}
		nextCursor = pagination.EncodeCursor(receivedAt, last.MessageID.String())
	}

	return results, nextCursor, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
