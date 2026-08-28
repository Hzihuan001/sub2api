package securityaudit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

type EventFilter struct {
	CaptureMode string     `json:"capture_mode,omitempty"`
	Decision    string     `json:"decision,omitempty"`
	RiskLevel   string     `json:"risk_level,omitempty"`
	Endpoint    string     `json:"endpoint,omitempty"`
	GroupID     *int64     `json:"group_id,omitempty"`
	UserID      *int64     `json:"user_id,omitempty"`
	APIKeyID    *int64     `json:"api_key_id,omitempty"`
	RequestID   string     `json:"request_id,omitempty"`
	PromptHash  string     `json:"prompt_hash,omitempty"`
	Keyword     string     `json:"keyword,omitempty"`
	StartAt     *time.Time `json:"start_at,omitempty"`
	EndAt       *time.Time `json:"end_at,omitempty"`
}

type RecordingStats struct {
	EventCount  int64      `json:"event_count"`
	PromptBytes int64      `json:"prompt_bytes"`
	OldestAt    *time.Time `json:"oldest_at,omitempty"`
	NewestAt    *time.Time `json:"newest_at,omitempty"`
}

type RecordingCleanupResult struct {
	DeletedEvents int64          `json:"deleted_events"`
	DeletedJobs   int64          `json:"deleted_jobs"`
	Remaining     RecordingStats `json:"remaining"`
}

type EventPage struct {
	Items    []*Event `json:"items"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Pages    int      `json:"pages"`
}

type DeletePreview struct {
	MatchedCount      int64       `json:"matched_count"`
	FilterSummary     EventFilter `json:"filter_summary"`
	SnapshotMaxID     int64       `json:"snapshot_max_id"`
	FilterHash        string      `json:"filter_hash"`
	ConfirmationToken string      `json:"confirmation_token,omitempty"`
	ExpiresAt         time.Time   `json:"expires_at,omitempty"`
}

type DeleteResult struct {
	DeletedEvents int64   `json:"deleted_events"`
	DeletedJobs   int64   `json:"deleted_jobs"`
	JobIDs        []int64 `json:"-"`
}

type EventRepository interface {
	ListEvents(ctx context.Context, filter EventFilter, page, pageSize int) (*EventPage, error)
	GetEvent(ctx context.Context, id int64) (*Event, error)
	DeleteEvent(ctx context.Context, id int64) (*DeleteResult, error)
	DeleteEventsByIDs(ctx context.Context, ids []int64) (*DeleteResult, error)
	PreviewDelete(ctx context.Context, filter EventFilter) (*DeletePreview, error)
	DeleteEventsByFilter(ctx context.Context, filter EventFilter, snapshotMaxID int64, batchSize int) (*DeleteResult, error)
}

func (r *PostgreSQLRepository) ListEvents(ctx context.Context, filter EventFilter, page, pageSize int) (*EventPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	where, args := buildEventWhere(filter, 1)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM prompt_audit_events e`+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	queryArgs := append([]any(nil), args...)
	limitIndex := len(queryArgs) + 1
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT `+eventListColumns("e")+` FROM prompt_audit_events e`+where+
		fmt.Sprintf(` ORDER BY e.created_at DESC, e.id DESC LIMIT $%d OFFSET $%d`, limitIndex, limitIndex+1), queryArgs...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*Event, 0, pageSize)
	for rows.Next() {
		event, err := scanEvent(rows, true)
		if err != nil {
			return nil, err
		}
		prepareEventForList(event)
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &EventPage{Items: items, Total: total, Page: page, PageSize: pageSize, Pages: pages}, nil
}

func (r *PostgreSQLRepository) GetEvent(ctx context.Context, id int64) (*Event, error) {
	event, err := scanEvent(r.db.QueryRowContext(ctx, `SELECT `+eventDetailColumns("e")+` FROM prompt_audit_events e WHERE e.id=$1`, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	return event, err
}

func (r *PostgreSQLRepository) CountEvents(ctx context.Context, filter EventFilter) (int64, error) {
	where, args := buildEventWhere(filter, 1)
	var total int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM prompt_audit_events e`+where, args...).Scan(&total)
	return total, err
}

// StreamEvents reads full prompt bodies one row at a time so exports do not
// scale process memory with the amount of sensitive text being exported.
func (r *PostgreSQLRepository) StreamEvents(ctx context.Context, filter EventFilter, limit int, consume func(*Event) error) (int64, error) {
	if limit < 1 || limit > 10000 {
		limit = 10000
	}
	where, args := buildEventWhere(filter, 1)
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, `SELECT `+eventDetailColumns("e")+` FROM prompt_audit_events e`+where+
		fmt.Sprintf(` ORDER BY e.created_at DESC, e.id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var exported int64
	for rows.Next() {
		event, err := scanEvent(rows, true)
		if err != nil {
			return exported, err
		}
		if err := consume(event); err != nil {
			return exported, err
		}
		exported++
	}
	return exported, rows.Err()
}

func (r *PostgreSQLRepository) RecordingStats(ctx context.Context) (RecordingStats, error) {
	var stats RecordingStats
	var oldest, newest sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(prompt_bytes),0), MIN(created_at), MAX(created_at)
		FROM prompt_audit_events WHERE capture_mode='capture_only'`).Scan(
		&stats.EventCount, &stats.PromptBytes, &oldest, &newest)
	if err != nil {
		return RecordingStats{}, err
	}
	if oldest.Valid {
		value := oldest.Time
		stats.OldestAt = &value
	}
	if newest.Valid {
		value := newest.Time
		stats.NewestAt = &value
	}
	return stats, nil
}

// CleanupRecording enforces both age and prompt-content byte limits. Oldest
// passive records are deleted first in bounded transactions.
func (r *PostgreSQLRepository) CleanupRecording(ctx context.Context, retentionDays, maxStorageMB, batchSize int) (*RecordingCleanupResult, error) {
	if retentionDays < MinRetentionDays || retentionDays > MaxRetentionDays {
		retentionDays = DefaultRetentionDays
	}
	if maxStorageMB < MinMaxStorageMB || maxStorageMB > MaxMaxStorageMB {
		maxStorageMB = DefaultMaxStorageMB
	}
	if batchSize < 1 || batchSize > 1000 {
		batchSize = 500
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	maxBytes := int64(maxStorageMB) * 1024 * 1024
	result := &RecordingCleanupResult{}
	for batch := 0; batch < 1000; batch++ {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		rows, err := tx.QueryContext(ctx, `
			WITH ranked AS (
				SELECT id, created_at,
					SUM(prompt_bytes) OVER (ORDER BY created_at DESC, id DESC) AS newest_bytes
				FROM prompt_audit_events WHERE capture_mode='capture_only'
			), selected AS (
				SELECT id FROM ranked
				WHERE created_at < $1 OR newest_bytes > $2
				ORDER BY created_at, id LIMIT $3
			), deleted AS (
				DELETE FROM prompt_audit_events e USING selected s
				WHERE e.id=s.id RETURNING e.job_id
			) SELECT job_id FROM deleted`, cutoff, maxBytes, batchSize)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		jobIDs, err := scanReturnedJobIDs(rows)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		deletedJobs, err := deleteOrphanJobs(ctx, tx, jobIDs)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		result.DeletedEvents += int64(len(jobIDs))
		result.DeletedJobs += deletedJobs
		if len(jobIDs) < batchSize {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	remaining, err := r.RecordingStats(ctx)
	if err != nil {
		return nil, err
	}
	result.Remaining = remaining
	return result, nil
}

func (r *PostgreSQLRepository) DeleteEvent(ctx context.Context, id int64) (*DeleteResult, error) {
	return r.DeleteEventsByIDs(ctx, []int64{id})
}

func (r *PostgreSQLRepository) DeleteEventsByIDs(ctx context.Context, ids []int64) (*DeleteResult, error) {
	ids = canonicalInt64s(ids)
	if len(ids) == 0 {
		return &DeleteResult{}, nil
	}
	if len(ids) > 500 {
		return nil, errors.New("prompt audit delete batch exceeds 500 events")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `DELETE FROM prompt_audit_events WHERE id=ANY($1) RETURNING job_id`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	jobIDs, err := scanReturnedJobIDs(rows)
	if err != nil {
		return nil, err
	}
	deletedJobs, err := deleteOrphanJobs(ctx, tx, jobIDs)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &DeleteResult{DeletedEvents: int64(len(jobIDs)), DeletedJobs: deletedJobs, JobIDs: canonicalInt64s(jobIDs)}, nil
}

func (r *PostgreSQLRepository) PreviewDelete(ctx context.Context, filter EventFilter) (*DeletePreview, error) {
	if err := validateDeleteFilter(filter); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	where, args := buildEventWhere(filter, 1)
	var count, maxID int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(e.id),0) FROM prompt_audit_events e`+where, args...).Scan(&count, &maxID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	canonical := canonicalEventFilter(filter)
	return &DeletePreview{MatchedCount: count, FilterSummary: canonical, SnapshotMaxID: maxID, FilterHash: FilterHash(canonical, maxID)}, nil
}

func (r *PostgreSQLRepository) DeleteEventsByFilter(ctx context.Context, filter EventFilter, snapshotMaxID int64, batchSize int) (*DeleteResult, error) {
	if err := validateDeleteFilter(filter); err != nil {
		return nil, err
	}
	if snapshotMaxID <= 0 {
		return &DeleteResult{}, nil
	}
	if batchSize < 1 || batchSize > 1000 {
		batchSize = 200
	}
	total := &DeleteResult{}
	jobSet := map[int64]struct{}{}
	for {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		where, args := buildEventWhere(filter, 1)
		maxIndex := len(args) + 1
		limitIndex := maxIndex + 1
		args = append(args, snapshotMaxID, batchSize)
		rows, err := tx.QueryContext(ctx, `
			WITH selected AS (
				SELECT e.id FROM prompt_audit_events e`+where+
			fmt.Sprintf(` AND e.id <= $%d ORDER BY e.id LIMIT $%d FOR UPDATE SKIP LOCKED`, maxIndex, limitIndex)+`
			), deleted AS (
				DELETE FROM prompt_audit_events e USING selected s WHERE e.id=s.id RETURNING e.job_id
			) SELECT job_id FROM deleted`, args...)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		jobIDs, err := scanReturnedJobIDs(rows)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		deletedJobs, err := deleteOrphanJobs(ctx, tx, jobIDs)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		total.DeletedEvents += int64(len(jobIDs))
		total.DeletedJobs += deletedJobs
		for _, id := range jobIDs {
			jobSet[id] = struct{}{}
		}
		if len(jobIDs) < batchSize {
			break
		}
	}
	for id := range jobSet {
		total.JobIDs = append(total.JobIDs, id)
	}
	total.JobIDs = canonicalInt64s(total.JobIDs)
	return total, nil
}

func FilterHash(filter EventFilter, snapshotMaxID int64) string {
	payload := struct {
		Filter        EventFilter `json:"filter"`
		SnapshotMaxID int64       `json:"snapshot_max_id"`
	}{canonicalEventFilter(filter), snapshotMaxID}
	raw, _ := json.Marshal(payload)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func validateDeleteFilter(filter EventFilter) error {
	if filter.StartAt == nil || filter.EndAt == nil || !filter.StartAt.Before(*filter.EndAt) {
		return errors.New("prompt audit filter delete requires a valid explicit time range")
	}
	return nil
}

func canonicalEventFilter(filter EventFilter) EventFilter {
	filter.CaptureMode = strings.TrimSpace(strings.ToLower(filter.CaptureMode))
	filter.Decision = strings.TrimSpace(strings.ToLower(filter.Decision))
	filter.RiskLevel = strings.TrimSpace(strings.ToLower(filter.RiskLevel))
	filter.Endpoint = strings.TrimSpace(filter.Endpoint)
	filter.RequestID = strings.TrimSpace(filter.RequestID)
	filter.PromptHash = strings.ToLower(strings.TrimSpace(filter.PromptHash))
	filter.Keyword = strings.TrimSpace(filter.Keyword)
	if filter.StartAt != nil {
		value := filter.StartAt.UTC()
		filter.StartAt = &value
	}
	if filter.EndAt != nil {
		value := filter.EndAt.UTC()
		filter.EndAt = &value
	}
	return filter
}

func buildEventWhere(filter EventFilter, firstIndex int) (string, []any) {
	filter = canonicalEventFilter(filter)
	clauses := []string{" WHERE TRUE"}
	args := make([]any, 0, 12)
	add := func(clause string, value any) {
		clauses = append(clauses, fmt.Sprintf(clause, firstIndex+len(args)))
		args = append(args, value)
	}
	if filter.CaptureMode != "" {
		add(" AND e.capture_mode=$%d", filter.CaptureMode)
	}
	if filter.Decision != "" {
		add(" AND e.decision=$%d", filter.Decision)
	}
	if filter.RiskLevel != "" {
		add(" AND e.risk_level=$%d", filter.RiskLevel)
	}
	if filter.Endpoint != "" {
		add(" AND e.endpoint=$%d", filter.Endpoint)
	}
	if filter.GroupID != nil {
		add(" AND e.group_id=$%d", *filter.GroupID)
	}
	if filter.UserID != nil {
		add(" AND e.user_id=$%d", *filter.UserID)
	}
	if filter.APIKeyID != nil {
		add(" AND e.api_key_id=$%d", *filter.APIKeyID)
	}
	if filter.RequestID != "" {
		add(" AND e.request_id=$%d", filter.RequestID)
	}
	if filter.PromptHash != "" {
		add(" AND e.prompt_hash=$%d", filter.PromptHash)
	}
	if filter.Keyword != "" {
		add(` AND (e.request_id ILIKE $%d OR e.prompt_hash ILIKE $%d OR e.redacted_preview ILIKE $%d
			OR e.username_snapshot ILIKE $%d OR e.user_email_snapshot ILIKE $%d OR e.api_key_name_snapshot ILIKE $%d
			OR e.model ILIKE $%d)`, "%"+TrimRunes(filter.Keyword, 128)+"%")
		// Rebuild the clause with one shared placeholder.
		clauses[len(clauses)-1] = fmt.Sprintf(` AND (e.request_id ILIKE $%[1]d OR e.prompt_hash ILIKE $%[1]d OR e.redacted_preview ILIKE $%[1]d
			OR e.username_snapshot ILIKE $%[1]d OR e.user_email_snapshot ILIKE $%[1]d OR e.api_key_name_snapshot ILIKE $%[1]d
			OR e.model ILIKE $%[1]d)`, firstIndex+len(args)-1)
	}
	if filter.StartAt != nil {
		add(" AND e.created_at >= $%d", filter.StartAt.UTC())
	}
	if filter.EndAt != nil {
		add(" AND e.created_at <= $%d", filter.EndAt.UTC())
	}
	return strings.Join(clauses, ""), args
}

func eventColumns(alias string) string {
	return fmt.Sprintf(`%[1]s.id,%[1]s.job_id,%[1]s.request_id,%[1]s.user_id,%[1]s.username_snapshot,
		%[1]s.user_email_snapshot,%[1]s.api_key_id,%[1]s.api_key_name_snapshot,%[1]s.group_id,%[1]s.group_name,
		%[1]s.provider,%[1]s.endpoint,%[1]s.protocol,%[1]s.model,%[1]s.prompt_hash,%[1]s.redacted_preview,
		%[1]s.stage,%[1]s.decision,%[1]s.risk_level,%[1]s.action,%[1]s.categories,%[1]s.matched_scanners,
		%[1]s.scanner_scores,%[1]s.scanner_evidence,%[1]s.scanner_backend,%[1]s.scanner_version,
		%[1]s.guard_endpoint_id,%[1]s.policy_id,%[1]s.policy_version,%[1]s.config_version,
		%[1]s.chunk_total,%[1]s.latency_ms,%[1]s.capture_mode,%[1]s.prompt_bytes,%[1]s.created_at`, alias)
}

// eventListColumns conditionally loads the full prompt only for legacy
// capture-only rows whose old preview policy stored a bare "***" marker. Those
// prompts are shorter than the withholding threshold, so this avoids loading
// ordinary large prompt bodies while letting the server derive a useful,
// sanitized preview without a production backfill.
func eventListColumns(alias string) string {
	return eventColumns(alias) + fmt.Sprintf(`,CASE
		WHEN %[1]s.capture_mode='capture_only' AND %[1]s.redacted_preview IN ('***','***…')
		THEN %[1]s.full_prompt ELSE '' END`, alias)
}

// eventDetailColumns adds the full prompt, which can be large, for explicit
// single-event detail reads. ListEvents uses eventListColumns instead and only
// conditionally reads legacy short masked capture-only rows.
func eventDetailColumns(alias string) string {
	return eventColumns(alias) + fmt.Sprintf(",%[1]s.full_prompt", alias)
}

func prepareEventForList(event *Event) {
	if event == nil {
		return
	}
	legacyMaskedPreview := event.Snapshot.RedactedPreview == "***" || event.Snapshot.RedactedPreview == "***…"
	if event.CaptureMode == "capture_only" && legacyMaskedPreview && event.Snapshot.FullPrompt != "" {
		event.Snapshot.RedactedPreview = BuildCaptureOnlyPreview(event.Snapshot.FullPrompt, DefaultCaptureOnlyPreviewMaxRunes)
	}
	// List responses must never expose the unredacted detail field.
	event.Snapshot.FullPrompt = ""
}

func scanEvent(row rowScanner, withFullPrompt ...bool) (*Event, error) {
	event := &Event{}
	var userID, apiKeyID, groupID sql.NullInt64
	var categories, matched, scores, evidence []byte
	dest := []any{&event.ID, &event.JobID, &event.Snapshot.RequestID, &userID,
		&event.Snapshot.UsernameSnapshot, &event.Snapshot.UserEmailSnapshot, &apiKeyID,
		&event.Snapshot.APIKeyNameSnapshot, &groupID, &event.Snapshot.GroupName,
		&event.Snapshot.Provider, &event.Snapshot.Endpoint, &event.Snapshot.Protocol, &event.Snapshot.Model,
		&event.Snapshot.PromptHash, &event.Snapshot.RedactedPreview, &event.Snapshot.Stage, &event.Decision,
		&event.RiskLevel, &event.Action, &categories, &matched, &scores, &evidence, &event.ScannerBackend,
		&event.ScannerVersion, &event.GuardEndpointID, &event.PolicyID, &event.PolicyVersion,
		&event.ConfigVersion, &event.ChunkTotal, &event.LatencyMS, &event.CaptureMode, &event.PromptBytes, &event.CreatedAt}
	if len(withFullPrompt) > 0 && withFullPrompt[0] {
		dest = append(dest, &event.Snapshot.FullPrompt)
	}
	err := row.Scan(dest...)
	if err != nil {
		return nil, err
	}
	event.Snapshot.UserID = nullableInt64Value(userID)
	event.Snapshot.APIKeyID = nullableInt64Value(apiKeyID)
	event.Snapshot.GroupID = nullableInt64Ptr(groupID)
	_ = json.Unmarshal(categories, &event.Categories)
	_ = json.Unmarshal(matched, &event.MatchedScanners)
	_ = json.Unmarshal(scores, &event.ScannerScores)
	_ = json.Unmarshal(evidence, &event.ScannerEvidence)
	result := NormalizedResult{Decision: event.Decision, RiskLevel: event.RiskLevel, Action: event.Action,
		Categories: event.Categories, MatchedScanners: event.MatchedScanners, ScannerScores: event.ScannerScores,
		ScannerEvidence: event.ScannerEvidence}
	event.IssueSummaries = BuildIssueSummaries(result)
	return event, nil
}

func scanReturnedJobIDs(rows *sql.Rows) ([]int64, error) {
	defer func() { _ = rows.Close() }()
	result := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func deleteOrphanJobs(ctx context.Context, tx *sql.Tx, jobIDs []int64) (int64, error) {
	jobIDs = canonicalInt64s(jobIDs)
	if len(jobIDs) == 0 {
		return 0, nil
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM prompt_audit_jobs j
		WHERE j.id=ANY($1) AND j.status <> 'processing'
		AND NOT EXISTS (SELECT 1 FROM prompt_audit_events e WHERE e.job_id=j.id)`, pq.Array(jobIDs))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
