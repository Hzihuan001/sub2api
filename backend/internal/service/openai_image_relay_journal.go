package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/imagerelay"
	_ "modernc.org/sqlite"
)

const (
	ImageRelayResultRetention = 15 * time.Minute
	imageRelayJournalLimit    = 65536 // 256 MiB at SQLite's 4 KiB page size.
	imageRelayJournalBatch    = 128
	imageRelayMaxSettlement   = 64 << 10
)

var (
	ErrImageRelayOperationNotFound = errors.New("image relay operation not found")
	ErrImageRelayOperationConflict = errors.New("image relay operation conflicts with the original request")
	ErrImageRelayOperationInvalid  = errors.New("invalid image relay operation")
	ErrImageRelayOperationState    = errors.New("image relay operation is not ready")
)

type ImageRelayOwner struct {
	UserID   int64
	APIKeyID int64
}

type ImageRelayOperationState string

const (
	ImageRelayPending     ImageRelayOperationState = "pending"
	ImageRelayResultReady ImageRelayOperationState = "result_ready"
	ImageRelaySettled     ImageRelayOperationState = "settled"
	ImageRelayFailed      ImageRelayOperationState = "failed"
	ImageRelayExpired     ImageRelayOperationState = "expired"
)

type ImageRelayStoredResult struct {
	BodyBytes   int64
	BodySHA256  string
	ContentType string
}

type ImageRelaySettlement struct {
	Command UsageBillingCommand
	Usage   UsageLog
}

type ImageRelayOperation struct {
	ID          string
	Owner       ImageRelayOwner
	Fingerprint string
	State       ImageRelayOperationState
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Result      *ImageRelayStoredResult
	Settlement  *ImageRelaySettlement
}

func (op ImageRelayOperation) Usable() bool {
	return op.State == ImageRelaySettled && op.Result != nil
}

type imageRelayJournal struct {
	db *sql.DB
}

// Keep a compact tombstone when result retention ends. Reusing an old
// idempotency key must not send a new paid upstream request.
func newImageRelayJournal(privateDir string) (*imageRelayJournal, error) {
	if !filepath.IsAbs(privateDir) {
		return nil, ErrImageRelayOperationInvalid
	}
	dir := filepath.Join(privateDir, "journal")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0700) {
		return nil, errors.New("image relay journal requires a private directory")
	}
	path := filepath.Join(dir, "operations.sqlite")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		if err = f.Close(); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		return nil, errors.New("image relay journal requires a private regular database")
	}
	databasePath := filepath.ToSlash(path)
	if !filepath.IsAbs(databasePath) || databasePath[0] != '/' {
		databasePath = "/" + databasePath
	}
	uri := url.URL{Scheme: "file", Path: databasePath}
	pragmas := url.Values{"_pragma": {
		"busy_timeout=5000",
		"page_size=4096",
		"journal_mode=DELETE",
		"synchronous=FULL",
		"secure_delete=ON",
		fmt.Sprintf("max_page_count=%d", imageRelayJournalLimit),
	}}
	uri.RawQuery = pragmas.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := imageRelayJournalContext()
	defer cancel()
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS image_relay_operations (
			id TEXT PRIMARY KEY NOT NULL,
			user_id INTEGER NOT NULL,
			api_key_id INTEGER NOT NULL,
			fingerprint TEXT NOT NULL,
			state TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			result BLOB,
			settlement BLOB
		)`,
		`CREATE INDEX IF NOT EXISTS image_relay_state_age ON image_relay_operations(state, updated_at, id)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &imageRelayJournal{db: db}, nil
}

func (j *imageRelayJournal) Close() error {
	return j.db.Close()
}

func imageRelayJournalContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 6*time.Second)
}

func validImageRelayOwner(owner ImageRelayOwner) bool {
	return owner.UserID > 0 && owner.APIKeyID > 0
}

func validImageRelayHex(v string, bytes int) bool {
	if len(v) != 2*bytes {
		return false
	}
	decoded, err := hex.DecodeString(v)
	return err == nil && hex.EncodeToString(decoded) == v
}

func imageRelayOperationID(owner ImageRelayOwner, key string) (string, error) {
	if key == "" {
		return imagerelay.NewOperationID()
	}
	if len(key) > 128 {
		return "", ErrImageRelayOperationInvalid
	}
	for i := range len(key) {
		if key[i] < '!' || key[i] > '~' || key[i] == ',' {
			return "", ErrImageRelayOperationInvalid
		}
	}
	value := fmt.Sprintf("image-relay-v1\x00%d\x00%d\x00%s", owner.UserID, owner.APIKeyID, key)
	return imagerelay.SHA256Hex([]byte(value))[:32], nil
}

func (j *imageRelayJournal) Begin(owner ImageRelayOwner, idempotencyKey, fingerprint string, now time.Time) (ImageRelayOperation, bool, error) {
	id, err := imageRelayOperationID(owner, idempotencyKey)
	if err != nil {
		return ImageRelayOperation{}, false, err
	}
	return j.BeginWithID(owner, id, fingerprint, now)
}

// BeginWithID accepts the same operation identifier on main after its verified
// private L1 request. Exactly one caller can authorize an upstream dispatch.
func (j *imageRelayJournal) BeginWithID(owner ImageRelayOwner, id, fingerprint string, now time.Time) (ImageRelayOperation, bool, error) {
	if !validImageRelayOwner(owner) || !validImageRelayHex(id, 16) ||
		!validImageRelayHex(fingerprint, 32) || now.UnixNano() <= 0 {
		return ImageRelayOperation{}, false, ErrImageRelayOperationInvalid
	}
	ctx, cancel := imageRelayJournalContext()
	defer cancel()
	inserted, err := j.db.ExecContext(ctx, `INSERT INTO image_relay_operations
		(id,user_id,api_key_id,fingerprint,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		id, owner.UserID, owner.APIKeyID, fingerprint, ImageRelayPending, now.UnixNano(), now.UnixNano())
	if err != nil {
		return ImageRelayOperation{}, false, err
	}
	count, err := inserted.RowsAffected()
	if err != nil {
		return ImageRelayOperation{}, false, err
	}
	op, err := j.lookup(ctx, owner, id)
	if err != nil {
		return ImageRelayOperation{}, false, err
	}
	if op.Fingerprint != fingerprint {
		return ImageRelayOperation{}, false, ErrImageRelayOperationConflict
	}
	return op, count == 1, nil
}

const imageRelayOpFields = "id,user_id,api_key_id,fingerprint,state,created_at,updated_at,result,settlement"

func scanImageRelayOperation(row interface{ Scan(...any) error }) (ImageRelayOperation, error) {
	var op ImageRelayOperation
	var created, updated int64
	var resultBytes, settlementBytes []byte
	err := row.Scan(&op.ID, &op.Owner.UserID, &op.Owner.APIKeyID, &op.Fingerprint, &op.State,
		&created, &updated, &resultBytes, &settlementBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return ImageRelayOperation{}, ErrImageRelayOperationNotFound
	}
	if err != nil {
		return ImageRelayOperation{}, err
	}
	op.CreatedAt, op.UpdatedAt = time.Unix(0, created), time.Unix(0, updated)
	if len(resultBytes) > 0 {
		op.Result = new(ImageRelayStoredResult)
		if err := json.Unmarshal(resultBytes, op.Result); err != nil {
			return ImageRelayOperation{}, err
		}
	}
	if len(settlementBytes) > 0 {
		op.Settlement = new(ImageRelaySettlement)
		if err := json.Unmarshal(settlementBytes, op.Settlement); err != nil {
			return ImageRelayOperation{}, err
		}
	}
	return op, nil
}

func (j *imageRelayJournal) Lookup(owner ImageRelayOwner, id string) (ImageRelayOperation, error) {
	ctx, cancel := imageRelayJournalContext()
	defer cancel()
	return j.lookup(ctx, owner, id)
}

func (j *imageRelayJournal) lookup(ctx context.Context, owner ImageRelayOwner, id string) (ImageRelayOperation, error) {
	if !validImageRelayOwner(owner) || !validImageRelayHex(id, 16) {
		return ImageRelayOperation{}, ErrImageRelayOperationNotFound
	}
	return scanImageRelayOperation(j.db.QueryRowContext(ctx, "SELECT "+imageRelayOpFields+
		" FROM image_relay_operations WHERE id=? AND user_id=? AND api_key_id=?", id, owner.UserID, owner.APIKeyID))
}

func (j *imageRelayJournal) MarkResult(owner ImageRelayOwner, id string, result ImageRelayStoredResult, now time.Time) error {
	mediaType, _, err := mime.ParseMediaType(result.ContentType)
	if err != nil || mediaType != "application/json" || len(result.ContentType) > 160 ||
		result.BodyBytes <= 0 || result.BodyBytes > imagerelay.MaxResultBodyBytes || !validImageRelayHex(result.BodySHA256, 32) {
		return ErrImageRelayOperationInvalid
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return j.change(owner, id, now, `UPDATE image_relay_operations SET state='result_ready', result=?, updated_at=?
		WHERE id=? AND user_id=? AND api_key_id=? AND state='pending'`, []any{encoded}, func(op ImageRelayOperation) bool {
		return (op.State == ImageRelayResultReady || op.State == ImageRelaySettled) && op.Result != nil && *op.Result == result
	})
}

func (j *imageRelayJournal) PrepareSettlement(owner ImageRelayOwner, id string, capture ImageRelaySettlement, now time.Time) error {
	if capture.Command.RequestID != "image-relay:"+id || capture.Usage.RequestID != capture.Command.RequestID ||
		capture.Command.UserID != owner.UserID || capture.Command.APIKeyID != owner.APIKeyID ||
		capture.Usage.UserID != owner.UserID || capture.Usage.APIKeyID != owner.APIKeyID ||
		capture.Command.AccountID <= 0 || capture.Usage.AccountID != capture.Command.AccountID {
		return ErrImageRelayOperationInvalid
	}
	capture.Command.Normalize()
	// These are ORM relations. Persist the event and identifiers, never the
	// related API key, account secrets or other credential records.
	capture.Usage.User, capture.Usage.APIKey, capture.Usage.Account = nil, nil, nil
	capture.Usage.Group, capture.Usage.Subscription = nil, nil
	encoded, err := json.Marshal(capture)
	if err != nil || len(encoded) > imageRelayMaxSettlement {
		return ErrImageRelayOperationInvalid
	}
	return j.change(owner, id, now, `UPDATE image_relay_operations SET settlement=?, updated_at=?
		WHERE id=? AND user_id=? AND api_key_id=? AND state='result_ready' AND settlement IS NULL`, []any{encoded},
		func(op ImageRelayOperation) bool {
			if op.Settlement == nil || (op.State != ImageRelayResultReady && op.State != ImageRelaySettled) {
				return false
			}
			old, err := json.Marshal(op.Settlement)
			return err == nil && bytes.Equal(encoded, old)
		})
}

func (j *imageRelayJournal) MarkSettled(owner ImageRelayOwner, id string, now time.Time) error {
	return j.change(owner, id, now, `UPDATE image_relay_operations SET state='settled', updated_at=?
		WHERE id=? AND user_id=? AND api_key_id=? AND state='result_ready' AND settlement IS NOT NULL`, nil,
		func(op ImageRelayOperation) bool { return op.State == ImageRelaySettled })
}

// MarkFailed is for a known refusal before dispatch. Ambiguous outcomes remain pending.
func (j *imageRelayJournal) MarkFailed(owner ImageRelayOwner, id string, now time.Time) error {
	return j.change(owner, id, now, `UPDATE image_relay_operations SET state='failed', updated_at=?
		WHERE id=? AND user_id=? AND api_key_id=? AND state='pending'`, nil,
		func(op ImageRelayOperation) bool { return op.State == ImageRelayFailed })
}

func (j *imageRelayJournal) change(owner ImageRelayOwner, id string, now time.Time, statement string, args []any,
	already func(ImageRelayOperation) bool) error {
	if !validImageRelayOwner(owner) || !validImageRelayHex(id, 16) || now.UnixNano() <= 0 {
		return ErrImageRelayOperationInvalid
	}
	ctx, cancel := imageRelayJournalContext()
	defer cancel()
	args = append(args, now.UnixNano(), id, owner.UserID, owner.APIKeyID)
	updated, err := j.db.ExecContext(ctx, statement, args...)
	if err != nil {
		return err
	}
	count, err := updated.RowsAffected()
	if err != nil || count > 0 {
		return err
	}
	op, err := j.lookup(ctx, owner, id)
	if err != nil {
		return err
	}
	if already(op) {
		return nil
	}
	return ErrImageRelayOperationState
}

func (j *imageRelayJournal) PendingSettlements(limit int) ([]ImageRelayOperation, error) {
	if limit < 1 || limit > imageRelayJournalBatch {
		return nil, ErrImageRelayOperationInvalid
	}
	ctx, cancel := imageRelayJournalContext()
	defer cancel()
	rows, err := j.db.QueryContext(ctx, "SELECT "+imageRelayOpFields+
		" FROM image_relay_operations WHERE state='result_ready' AND settlement IS NOT NULL ORDER BY updated_at,id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var operations []ImageRelayOperation
	for rows.Next() {
		op, err := scanImageRelayOperation(rows)
		if err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}
	return operations, rows.Err()
}

// Move unsuccessful attempts behind other pending records so that expired or
// unresolvable results cannot monopolize the bounded maintenance batch.
func (j *imageRelayJournal) deferSettlement(op ImageRelayOperation, now time.Time) error {
	ctx, cancel := imageRelayJournalContext()
	defer cancel()
	_, err := j.db.ExecContext(ctx, "UPDATE image_relay_operations SET updated_at=?"+
		" WHERE id=? AND user_id=? AND api_key_id=? AND state='result_ready' AND updated_at=?",
		now.UnixNano(), op.ID, op.Owner.UserID, op.Owner.APIKeyID, op.UpdatedAt.UnixNano())
	return err
}

func (j *imageRelayJournal) Prune(now time.Time) ([]string, error) {
	ctx, cancel := imageRelayJournalContext()
	defer cancel()
	rows, err := j.db.QueryContext(ctx, `UPDATE image_relay_operations
		SET state='expired', result=NULL, settlement=NULL
		WHERE id IN (SELECT id FROM image_relay_operations WHERE state='settled' AND updated_at <= ?
		ORDER BY updated_at,id LIMIT ?) RETURNING id`, now.Add(-ImageRelayResultRetention).UnixNano(), imageRelayJournalBatch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var expired []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		expired = append(expired, id)
	}
	return expired, rows.Err()
}

// Compose the exact request fingerprint before model/body normalization. In
// particular, an idempotency key must fail on a different image path or when
// the same multipart bytes are submitted with a different content type.
func imageRelayRequestFingerprint(path, contentType string, body []byte) string {
	return imagerelay.SHA256Hex([]byte(path + "\x00" + strings.TrimSpace(contentType) + "\x00" + imagerelay.SHA256Hex(body)))
}
