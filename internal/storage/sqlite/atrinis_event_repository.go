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
		ToolDeclarations: req.ToolDeclarations,
		ToolHash:         req.ToolHash,
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
    model_slot, content_hash, tool_declarations, tool_hash, requested_targets, resolved_targets, status,
    session_id, result, error, received_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		nullString(event.ToolDeclarations),
		nullString(event.ToolHash),
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

func (r *AtrinisEventRepository) Get(ctx context.Context, id string) (*storage.AtrinisEvent, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, event_key, token_name, atriwisp_name, source, source_id, tags, mode,
       model_slot, content_hash, tool_declarations, tool_hash, requested_targets, resolved_targets, status,
       session_id, result, error, received_at, created_at, updated_at
FROM atrinis_events
WHERE id = ?`, id)
	event, err := scanAtrinisEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get atrinis event by id: %w", err)
	}
	return event, nil
}

func (r *AtrinisEventRepository) GetByKey(ctx context.Context, atriwispName, source, sourceID string) (*storage.AtrinisEvent, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, event_key, token_name, atriwisp_name, source, source_id, tags, mode,
       model_slot, content_hash, tool_declarations, tool_hash, requested_targets, resolved_targets, status,
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

func (r *AtrinisEventRepository) PrepareReport(ctx context.Context, req storage.PrepareAtrinisReportRequest) error {
	if len(req.Deliveries) == 0 {
		return fmt.Errorf("prepare atrinis report: no deliveries")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin prepare atrinis report: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := storage.Now()
	res, err := tx.ExecContext(ctx, `
UPDATE atrinis_events
SET resolved_targets = ?, status = ?, session_id = ?, result = ?, error = NULL, updated_at = ?
WHERE id = ?`,
		nullString(req.ResolvedTargets),
		req.ResultReadyStatus,
		nullString(req.SessionID),
		nullString(req.Result),
		storage.FormatTime(now),
		req.EventID,
	)
	if err != nil {
		return fmt.Errorf("prepare atrinis report event: %w", err)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil && n == 0 {
		return storage.ErrNotFound
	}
	for ordinal, delivery := range req.Deliveries {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO atrinis_report_deliveries (
    id, event_id, ordinal, target, output, message_id, status, receipt, error, attempts, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL, 0, ?, ?)`,
			storage.NewID(),
			req.EventID,
			ordinal,
			delivery.Target,
			delivery.Output,
			nullString(delivery.MessageID),
			storage.AtrinisReportDeliveryPending,
			storage.FormatTime(now),
			storage.FormatTime(now),
		); err != nil {
			return fmt.Errorf("prepare atrinis report delivery %d: %w", ordinal, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit prepare atrinis report: %w", err)
	}
	return nil
}

func (r *AtrinisEventRepository) ResetDeliveringReports(ctx context.Context, deliveringStatus, resultReadyStatus string) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE atrinis_events
SET status = ?, updated_at = ?
WHERE status = ?
  AND EXISTS (SELECT 1 FROM atrinis_report_deliveries d WHERE d.event_id = atrinis_events.id)`,
		resultReadyStatus,
		storage.FormatTime(storage.Now()),
		deliveringStatus,
	)
	if err != nil {
		return fmt.Errorf("reset delivering atrinis reports: %w", err)
	}
	return nil
}

func (r *AtrinisEventRepository) ListResultReadyReportIDs(ctx context.Context, resultReadyStatus string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT e.id
FROM atrinis_events e
WHERE e.status = ?
  AND EXISTS (SELECT 1 FROM atrinis_report_deliveries d WHERE d.event_id = e.id)
ORDER BY e.updated_at, e.id`, resultReadyStatus)
	if err != nil {
		return nil, fmt.Errorf("list result-ready atrinis reports: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan result-ready atrinis report: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate result-ready atrinis reports: %w", err)
	}
	return ids, nil
}

func (r *AtrinisEventRepository) ClaimReport(ctx context.Context, eventID, resultReadyStatus, deliveringStatus string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
UPDATE atrinis_events
SET status = ?, error = NULL, updated_at = ?
WHERE id = ? AND status = ?`,
		deliveringStatus,
		storage.FormatTime(storage.Now()),
		eventID,
		resultReadyStatus,
	)
	if err != nil {
		return false, fmt.Errorf("claim atrinis report: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim atrinis report rows affected: %w", err)
	}
	return n > 0, nil
}

func (r *AtrinisEventRepository) ReleaseReport(ctx context.Context, eventID, deliveringStatus, resultReadyStatus, deliveryError string) error {
	res, err := r.db.ExecContext(ctx, `
UPDATE atrinis_events
SET status = ?, error = ?, updated_at = ?
WHERE id = ? AND status = ?`,
		resultReadyStatus,
		nullString(deliveryError),
		storage.FormatTime(storage.Now()),
		eventID,
		deliveringStatus,
	)
	if err != nil {
		return fmt.Errorf("release atrinis report: %w", err)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil && n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (r *AtrinisEventRepository) ListReportDeliveries(ctx context.Context, eventID string) ([]storage.AtrinisReportDelivery, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, event_id, ordinal, target, output, message_id, status, receipt, error, attempts, created_at, updated_at
FROM atrinis_report_deliveries
WHERE event_id = ?
ORDER BY ordinal`, eventID)
	if err != nil {
		return nil, fmt.Errorf("list atrinis report deliveries: %w", err)
	}
	defer rows.Close()
	var deliveries []storage.AtrinisReportDelivery
	for rows.Next() {
		delivery, err := scanAtrinisReportDelivery(rows)
		if err != nil {
			return nil, fmt.Errorf("scan atrinis report delivery: %w", err)
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate atrinis report deliveries: %w", err)
	}
	return deliveries, nil
}

func (r *AtrinisEventRepository) StartReportDelivery(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `
UPDATE atrinis_report_deliveries
SET attempts = attempts + 1, error = NULL, updated_at = ?
WHERE id = ? AND status != ?`,
		storage.FormatTime(storage.Now()),
		id,
		storage.AtrinisReportDeliveryDelivered,
	)
	if err != nil {
		return fmt.Errorf("start atrinis report delivery: %w", err)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil && n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (r *AtrinisEventRepository) MarkReportDeliveryFailed(ctx context.Context, eventID, deliveryID, resultReadyStatus, deliveryError string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin fail atrinis report delivery: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := storage.FormatTime(storage.Now())
	res, err := tx.ExecContext(ctx, `
UPDATE atrinis_report_deliveries
SET status = ?, error = ?, updated_at = ?
WHERE id = ? AND event_id = ? AND status != ?`,
		storage.AtrinisReportDeliveryFailed,
		nullString(deliveryError),
		now,
		deliveryID,
		eventID,
		storage.AtrinisReportDeliveryDelivered,
	)
	if err != nil {
		return fmt.Errorf("fail atrinis report delivery: %w", err)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil && n == 0 {
		return storage.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE atrinis_events
SET status = ?, error = ?, updated_at = ?
WHERE id = ?`, resultReadyStatus, nullString(deliveryError), now, eventID); err != nil {
		return fmt.Errorf("release failed atrinis report: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed atrinis report delivery: %w", err)
	}
	return nil
}

func (r *AtrinisEventRepository) MarkReportDeliveryDelivered(ctx context.Context, deliveryID, receipt string) error {
	res, err := r.db.ExecContext(ctx, `
UPDATE atrinis_report_deliveries
SET status = ?, receipt = ?, error = NULL, updated_at = ?
WHERE id = ? AND status != ?`,
		storage.AtrinisReportDeliveryDelivered,
		nullString(receipt),
		storage.FormatTime(storage.Now()),
		deliveryID,
		storage.AtrinisReportDeliveryDelivered,
	)
	if err != nil {
		return fmt.Errorf("complete atrinis report delivery: %w", err)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil && n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (r *AtrinisEventRepository) CompleteReport(ctx context.Context, eventID, deliveringStatus, completedStatus string) error {
	res, err := r.db.ExecContext(ctx, `
UPDATE atrinis_events
SET status = ?, error = NULL, updated_at = ?
WHERE id = ? AND status = ?
  AND NOT EXISTS (
      SELECT 1
      FROM atrinis_report_deliveries d
      WHERE d.event_id = atrinis_events.id AND d.status != ?
  )`,
		completedStatus,
		storage.FormatTime(storage.Now()),
		eventID,
		deliveringStatus,
		storage.AtrinisReportDeliveryDelivered,
	)
	if err != nil {
		return fmt.Errorf("complete atrinis report: %w", err)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil && n == 0 {
		return fmt.Errorf("complete atrinis report: deliveries are not complete")
	}
	return nil
}

func scanAtrinisReportDelivery(row interface{ Scan(dest ...any) error }) (storage.AtrinisReportDelivery, error) {
	var delivery storage.AtrinisReportDelivery
	var messageID, receipt, deliveryError sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&delivery.ID,
		&delivery.EventID,
		&delivery.Ordinal,
		&delivery.Target,
		&delivery.Output,
		&messageID,
		&delivery.Status,
		&receipt,
		&deliveryError,
		&delivery.Attempts,
		&createdAt,
		&updatedAt,
	); err != nil {
		return delivery, err
	}
	delivery.Receipt = receipt.String
	delivery.MessageID = messageID.String
	delivery.Error = deliveryError.String
	var err error
	delivery.CreatedAt, err = storage.ParseTime(createdAt)
	if err != nil {
		return delivery, fmt.Errorf("parse atrinis report delivery created_at: %w", err)
	}
	delivery.UpdatedAt, err = storage.ParseTime(updatedAt)
	if err != nil {
		return delivery, fmt.Errorf("parse atrinis report delivery updated_at: %w", err)
	}
	return delivery, nil
}

func scanAtrinisEvent(row interface{ Scan(dest ...any) error }) (*storage.AtrinisEvent, error) {
	var event storage.AtrinisEvent
	var tags, modelSlot, toolDeclarations, toolHash, requestedTargets, resolvedTargets, sessionID, result, eventErr sql.NullString
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
		&toolDeclarations,
		&toolHash,
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
	event.ToolDeclarations = toolDeclarations.String
	event.ToolHash = toolHash.String
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
