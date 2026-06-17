package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fujiwarashigure/ATRI-X/internal/storage"
)

type AtrinisEventRepository struct {
	db *sql.DB
}

func (r *AtrinisEventRepository) Create(ctx context.Context, req storage.CreateAtrinisEventRequest) (*storage.AtrinisEvent, error) {
	now := storage.Now()
	receivedAt := req.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = now
	}
	createdAt := req.CreatedAt
	if createdAt.IsZero() {
		createdAt = receivedAt
	}
	event := &storage.AtrinisEvent{
		ID:               storage.NewID(),
		EventKey:         req.EventKey,
		TokenName:        req.TokenName,
		AtriwispName:       req.AtriwispName,
		Source:           req.Source,
		SourceID:         req.SourceID,
		Tags:             req.Tags,
		Mode:             req.Mode,
		ModelSlot:        req.ModelSlot,
		ContentHash:      req.ContentHash,
		RequestedTargets: req.RequestedTargets,
		ResolvedTargets:  req.ResolvedTargets,
		Status:           req.Status,
		Result:           req.Result,
		Error:            req.Error,
		ReceivedAt:       receivedAt,
		CreatedAt:        createdAt,
		UpdatedAt:        now,
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO atrinis_events (
    id, event_key, token_name, atriwisp_name, source, source_id, tags, mode,
    model_slot, content_hash, requested_targets, resolved_targets, status,
    session_id, result, error, received_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.EventKey,
		event.TokenName,
		event.AtriwispName,
		event.Source,
		event.SourceID,
		nullString(event.Tags),
		event.Mode,
		nullString(event.ModelSlot),
		event.ContentHash,
		nullString(event.RequestedTargets),
		nullString(event.ResolvedTargets),
		event.Status,
		nullString(event.SessionID),
		nullString(event.Result),
		nullString(event.Error),
		storage.FormatTime(event.ReceivedAt),
		storage.FormatTime(event.CreatedAt),
		storage.FormatTime(event.UpdatedAt),
	)
	if err != nil {
		return nil, fmt.Errorf("create atrinis event: %w", err)
	}
	return event, nil
}

func (r *AtrinisEventRepository) GetByKey(ctx context.Context, atriwispName, source, sourceID string) (*storage.AtrinisEvent, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, event_key, token_name, atriwisp_name, source, source_id, tags, mode,
       model_slot, content_hash, requested_targets, resolved_targets, status,
       session_id, result, error, received_at, created_at, updated_at
FROM atrinis_events
WHERE atriwisp_name = ? AND source = ? AND source_id = ?`, atriwispName, source, sourceID)
	event, err := scanAtrinisEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get atrinis event: %w", err)
	}
	return event, nil
}

func (r *AtrinisEventRepository) Update(ctx context.Context, req storage.UpdateAtrinisEventRequest) error {
	res, err := r.db.ExecContext(ctx, `
UPDATE atrinis_events
SET resolved_targets = ?, status = ?, session_id = ?, result = ?, error = ?, updated_at = ?
WHERE id = ?`,
		nullString(req.ResolvedTargets),
		req.Status,
		nullString(req.SessionID),
		nullString(req.Result),
		nullString(req.Error),
		storage.FormatTime(storage.Now()),
		req.ID,
	)
	if err != nil {
		return fmt.Errorf("update atrinis event: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func scanAtrinisEvent(row interface{ Scan(dest ...any) error }) (*storage.AtrinisEvent, error) {
	var event storage.AtrinisEvent
	var tags, modelSlot, requestedTargets, resolvedTargets, sessionID, result, eventErr sql.NullString
	var receivedAt, createdAt, updatedAt string
	if err := row.Scan(
		&event.ID,
		&event.EventKey,
		&event.TokenName,
		&event.AtriwispName,
		&event.Source,
		&event.SourceID,
		&tags,
		&event.Mode,
		&modelSlot,
		&event.ContentHash,
		&requestedTargets,
		&resolvedTargets,
		&event.Status,
		&sessionID,
		&result,
		&eventErr,
		&receivedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	event.Tags = tags.String
	event.ModelSlot = modelSlot.String
	event.RequestedTargets = requestedTargets.String
	event.ResolvedTargets = resolvedTargets.String
	event.SessionID = sessionID.String
	event.Result = result.String
	event.Error = eventErr.String

	var err error
	event.ReceivedAt, err = storage.ParseTime(receivedAt)
	if err != nil {
		return nil, fmt.Errorf("parse atrinis event received_at: %w", err)
	}
	event.CreatedAt, err = storage.ParseTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse atrinis event created_at: %w", err)
	}
	event.UpdatedAt, err = storage.ParseTime(updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse atrinis event updated_at: %w", err)
	}
	return &event, nil
}
