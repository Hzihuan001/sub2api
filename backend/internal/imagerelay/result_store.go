package imagerelay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrResultCapacity  = errors.New("image relay result capacity exhausted")
	ErrResultExists    = errors.New("image relay result already exists or is reserved")
	ErrResultNotFound  = errors.New("image relay result not found")
	ErrResultIntegrity = errors.New("image relay stored result failed integrity checks")
	ErrResultStorage   = errors.New("image relay result storage unavailable")
	ErrResultBusy      = errors.New("image relay result storage has active users")
)

const (
	resultMagic       = "IMGRSLT1"
	resultPrefixSize  = len(resultMagic) + 4
	resultHeaderLimit = 16 << 10
	resultMaxOverhead = int64(resultPrefixSize + resultHeaderLimit)
)

// ResultMetadata describes the verified body. Save supplies the creation and
// expiration times. Proof may contain internal usage metadata for recovery.
type ResultMetadata struct {
	Status      int             `json:"status"`
	BodyBytes   int64           `json:"body_bytes"`
	BodySHA256  string          `json:"body_sha256"`
	ContentType string          `json:"content_type"`
	CreatedAt   time.Time       `json:"created_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
	Proof       json.RawMessage `json:"proof,omitempty"`
}

type resultHeader struct {
	OperationID string         `json:"operation_id"`
	APIKeyID    int64          `json:"api_key_id"`
	Metadata    ResultMetadata `json:"metadata"`
}

type resultEntry struct {
	size      int64
	expiresAt time.Time
	readers   int
}

// ResultStore owns a dedicated private directory on the application's
// persistent volume. One process may own the directory at a time. The byte
// quota includes the framing metadata plus the body of any retained result.
type ResultStore struct {
	root         *os.Root
	lock         *os.File
	maxFiles     int
	maxBytes     int64
	minFreeBytes int64
	retention    time.Duration

	mu       sync.Mutex
	entries  map[string]*resultEntry
	pending  map[string]int64
	bytes    int64
	reserved int64
	readers  int
	closed   bool
	fault    error
}

func NewResultStore(path string, maxFiles int, maxBytes, minFreeBytes int64, retention time.Duration) (*ResultStore, error) {
	if path == "" || maxFiles <= 0 || maxBytes <= resultMaxOverhead || minFreeBytes < 0 || retention <= 0 {
		return nil, ErrResultStorage
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.Join(ErrResultStorage, err)
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, errors.Join(ErrResultStorage, err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, errors.Join(ErrResultStorage, err)
	}
	if !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0700) {
		return nil, fmt.Errorf("%w: result directory must have mode 0700", ErrResultStorage)
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, errors.Join(ErrResultStorage, err)
	}
	bound, err := root.Stat(".")
	if err != nil || !os.SameFile(info, bound) {
		_ = root.Close()
		return nil, fmt.Errorf("%w: result directory changed while opening", ErrResultStorage)
	}
	lock, err := lockSpool(root)
	if err != nil {
		_ = root.Close()
		return nil, errors.Join(ErrResultStorage, err)
	}
	store := &ResultStore{root: root, lock: lock, maxFiles: maxFiles, maxBytes: maxBytes, minFreeBytes: minFreeBytes,
		retention: retention, entries: make(map[string]*resultEntry), pending: make(map[string]int64)}
	if err := store.recover(filepath.Dir(absolute)); err != nil {
		_ = store.Close()
		return nil, errors.Join(ErrResultStorage, err)
	}
	return store, nil
}

func (s *ResultStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if len(s.pending) > 0 || s.readers > 0 {
		return ErrResultBusy
	}
	s.closed = true
	return errors.Join(unlockSpool(s.root, s.lock), s.root.Close())
}

func resultFileName(operationID string, apiKeyID int64) string {
	return SHA256Hex([]byte(strconv.FormatInt(apiKeyID, 10)+"\x00"+operationID)) + ".result"
}

// Reserve holds maximum framed bytes and one file slot before paid dispatch.
func (s *ResultStore) Reserve(operationID string, apiKeyID int64, maxExpectedBytes int64) (*ResultReservation, error) {
	if !operationPattern.MatchString(operationID) || apiKeyID <= 0 {
		return nil, ErrInvalidBinding
	}
	if maxExpectedBytes < 1 || maxExpectedBytes > MaxResultBodyBytes {
		return nil, ErrResultCapacity
	}
	name := resultFileName(operationID, apiKeyID)
	budget := maxExpectedBytes + resultMaxOverhead
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.fault != nil {
		return nil, ErrResultStorage
	}
	if s.entries[name] != nil || s.pending[name] != 0 {
		return nil, ErrResultExists
	}
	if len(s.entries)+len(s.pending) >= s.maxFiles || budget > s.maxBytes-s.bytes-s.reserved {
		return nil, ErrResultCapacity
	}
	if s.minFreeBytes > 0 {
		free, err := spoolFreeBytes(s.root)
		if err != nil {
			return nil, errors.Join(ErrResultCapacity, err)
		}
		if free < uint64(s.minFreeBytes) || uint64(s.reserved+budget) > free-uint64(s.minFreeBytes) {
			return nil, ErrResultCapacity
		}
	}
	s.pending[name] = budget
	s.reserved += budget
	return &ResultReservation{store: s, name: name, operationID: operationID, apiKeyID: apiKeyID, maxBytes: maxExpectedBytes}, nil
}

type ResultReservation struct {
	store       *ResultStore
	name        string
	operationID string
	apiKeyID    int64
	maxBytes    int64
	mu          sync.Mutex
	finished    bool
}

func (r *ResultReservation) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.release()
	return nil
}

func (r *ResultReservation) release() {
	if r.finished {
		return
	}
	r.finished = true
	s := r.store
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserved -= s.pending[r.name]
	delete(s.pending, r.name)
}

// Save verifies and writes a result in one pass. The successful result becomes
// visible only after syncing the file, renaming it, and syncing the directory.
func (r *ResultReservation) Save(ctx context.Context, body io.Reader, metadata ResultMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return ErrResultExists
	}
	defer r.release()
	if err := ctx.Err(); err != nil {
		return err
	}
	s := r.store
	metadata.CreatedAt = time.Now().UTC()
	metadata.ExpiresAt = metadata.CreatedAt.Add(s.retention)
	if body == nil || metadata.BodyBytes > r.maxBytes || !validResultMetadata(metadata) {
		return ErrResultIntegrity
	}
	header := resultHeader{OperationID: r.operationID, APIKeyID: r.apiKeyID, Metadata: metadata}
	encoded, err := json.Marshal(header)
	if err != nil || len(encoded) > resultHeaderLimit {
		return ErrResultIntegrity
	}
	if closer, ok := body.(io.ReadCloser); ok {
		stop := context.AfterFunc(ctx, func() { _ = closer.Close() })
		defer stop()
	}
	staging := r.name + ".part"
	file, err := s.root.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.Join(ErrResultStorage, err)
	}
	defer func() {
		if err := s.root.Remove(staging); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.mu.Lock()
			s.fault = err
			s.mu.Unlock()
		}
	}()
	prefix := [resultPrefixSize]byte{}
	copy(prefix[:], resultMagic)
	binary.BigEndian.PutUint32(prefix[len(resultMagic):], uint32(len(encoded)))
	writeErr := writeResult(ctx, file, body, prefix[:], encoded, metadata)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fault != nil {
		return ErrResultStorage
	}
	if _, err := s.root.Lstat(r.name); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return ErrResultExists
		}
		return errors.Join(ErrResultStorage, err)
	}
	if err := s.root.Rename(staging, r.name); err != nil {
		return errors.Join(ErrResultStorage, err)
	}
	if err := s.syncDirectory(); err != nil {
		s.fault = err
		return errors.Join(ErrResultStorage, err)
	}
	size := metadata.BodyBytes + int64(len(prefix)+len(encoded))
	s.entries[r.name] = &resultEntry{size: size, expiresAt: metadata.ExpiresAt}
	s.bytes += size
	return nil
}

func writeResult(ctx context.Context, file *os.File, body io.Reader, prefix, encoded []byte, metadata ResultMetadata) error {
	if _, err := file.Write(prefix); err != nil {
		return errors.Join(ErrResultStorage, err)
	}
	if _, err := file.Write(encoded); err != nil {
		return errors.Join(ErrResultStorage, err)
	}
	if err := checkResultBody(ctx, file, body, metadata); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return errors.Join(ErrResultStorage, err)
	}
	return nil
}

func validResultMetadata(metadata ResultMetadata) bool {
	mediaType, _, err := mime.ParseMediaType(metadata.ContentType)
	return err == nil && len(metadata.ContentType) <= 255 && mediaType == "application/json" &&
		metadata.Status >= 200 && metadata.Status < 300 && metadata.BodyBytes > 0 && metadata.BodyBytes <= MaxResultBodyBytes &&
		digestPattern.MatchString(metadata.BodySHA256) && !metadata.CreatedAt.IsZero() && metadata.ExpiresAt.After(metadata.CreatedAt)
}

func checkResultBody(ctx context.Context, dst io.Writer, src io.Reader, metadata ResultMetadata) error {
	hash := sha256.New()
	input := spoolContextReader{ctx: ctx, src: src}
	n, err := io.CopyBuffer(io.MultiWriter(dst, hash), io.LimitReader(input, metadata.BodyBytes), make([]byte, spoolBufferBytes))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || n != metadata.BodyBytes || hex.EncodeToString(hash.Sum(nil)) != metadata.BodySHA256 {
		return errors.Join(ErrResultIntegrity, err)
	}
	var extra [1]byte
	if _, err := io.ReadFull(input, extra[:]); err != io.EOF {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.Join(ErrResultIntegrity, err)
	}
	return nil
}

// StoredResult exposes the body only after its on-disk framing and digest have
// been checked. Closing it releases the reader without deleting the result.
type StoredResult struct {
	io.Reader
	Metadata ResultMetadata

	file  *os.File
	store *ResultStore
	entry *resultEntry
	once  sync.Once
	err   error
}

func (r *StoredResult) Close() error {
	r.once.Do(func() {
		r.err = r.file.Close()
		r.store.releaseReader(r.entry)
	})
	return r.err
}

func (s *ResultStore) Open(ctx context.Context, operationID string, apiKeyID int64) (*StoredResult, error) {
	if !operationPattern.MatchString(operationID) || apiKeyID <= 0 {
		return nil, ErrResultNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := resultFileName(operationID, apiKeyID)
	s.mu.Lock()
	if s.closed || s.fault != nil {
		s.mu.Unlock()
		return nil, ErrResultStorage
	}
	entry := s.entries[name]
	if entry == nil || !time.Now().Before(entry.expiresAt) {
		s.mu.Unlock()
		return nil, ErrResultNotFound
	}
	entry.readers++
	s.readers++
	s.mu.Unlock()

	file, header, err := s.openResultFile(name)
	if err != nil {
		s.releaseReader(entry)
		return nil, err
	}
	metadata := header.Metadata
	if !time.Now().Before(s.expiresAt(metadata)) {
		_ = file.Close()
		s.releaseReader(entry)
		return nil, ErrResultNotFound
	}
	offset, err := file.Seek(0, io.SeekCurrent)
	if err == nil {
		err = checkResultBody(ctx, io.Discard, file, metadata)
	}
	if err != nil {
		_ = file.Close()
		s.releaseReader(entry)
		return nil, err
	}
	return &StoredResult{Reader: io.NewSectionReader(file, offset, metadata.BodyBytes), Metadata: metadata,
		file: file, store: s, entry: entry}, nil
}

func (s *ResultStore) releaseReader(entry *resultEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.readers--
	s.readers--
}

func (s *ResultStore) expiresAt(metadata ResultMetadata) time.Time {
	cutoff := metadata.CreatedAt.Add(s.retention)
	if metadata.ExpiresAt.Before(cutoff) {
		return metadata.ExpiresAt
	}
	return cutoff
}

func (s *ResultStore) openResultFile(name string) (*os.File, resultHeader, error) {
	var header resultHeader
	current, err := s.root.Lstat(name)
	if err != nil {
		return nil, header, errors.Join(ErrResultStorage, err)
	}
	if !current.Mode().IsRegular() || (runtime.GOOS != "windows" && current.Mode().Perm() != 0600) {
		return nil, header, ErrResultIntegrity
	}
	file, err := s.root.Open(name)
	if err != nil {
		return nil, header, errors.Join(ErrResultStorage, err)
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(current, opened) {
		_ = file.Close()
		return nil, header, errors.Join(ErrResultIntegrity, err)
	}
	header, offset, err := readResultHeader(file)
	if err != nil || opened.Size() != offset+header.Metadata.BodyBytes ||
		!operationPattern.MatchString(header.OperationID) || header.APIKeyID <= 0 ||
		resultFileName(header.OperationID, header.APIKeyID) != name || !validResultMetadata(header.Metadata) {
		_ = file.Close()
		return nil, resultHeader{}, errors.Join(ErrResultIntegrity, err)
	}
	return file, header, nil
}

func readResultHeader(file *os.File) (resultHeader, int64, error) {
	var header resultHeader
	prefix := [resultPrefixSize]byte{}
	if _, err := io.ReadFull(file, prefix[:]); err != nil {
		return header, 0, err
	}
	length := binary.BigEndian.Uint32(prefix[len(resultMagic):])
	if string(prefix[:len(resultMagic)]) != resultMagic || length == 0 || length > resultHeaderLimit {
		return header, 0, ErrResultIntegrity
	}
	encoded := make([]byte, length)
	if _, err := io.ReadFull(file, encoded); err != nil {
		return header, 0, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&header); err != nil {
		return header, 0, err
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF {
		return header, 0, ErrResultIntegrity
	}
	return header, int64(resultPrefixSize) + int64(length), nil
}

func (s *ResultStore) recover(parentPath string) error {
	parent, err := os.Open(parentPath)
	if err != nil {
		return err
	}
	if err := errors.Join(syncClaimDirectory(parent), parent.Close()); err != nil {
		return err
	}
	dir, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	var abandoned []string
	for {
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			if entry.Name() == spoolLockName {
				continue
			}
			if !entry.Type().IsRegular() {
				return errors.New("unexpected entry in image relay result directory")
			}
			name, staging := strings.CutSuffix(entry.Name(), ".part")
			prefix, result := strings.CutSuffix(name, ".result")
			if !result || !digestPattern.MatchString(prefix) {
				return errors.New("unexpected filename in image relay result directory")
			}
			if staging {
				abandoned = append(abandoned, entry.Name())
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			// Invalid finalized files keep their quota until expiry. Open will
			// reject them independently, so a corrupt image cannot block recovery
			// of every other operation.
			expires := info.ModTime().Add(s.retention)
			file, header, err := s.openResultFile(name)
			if err == nil {
				_ = file.Close()
				expires = s.expiresAt(header.Metadata)
			} else if !errors.Is(err, ErrResultIntegrity) {
				return err
			}
			s.entries[name] = &resultEntry{size: info.Size(), expiresAt: expires}
			s.bytes += info.Size()
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := dir.Close(); err != nil {
		return err
	}
	for _, name := range abandoned {
		if err := s.root.Remove(name); err != nil {
			return err
		}
	}
	if _, err := s.Prune(time.Now()); err != nil {
		return err
	}
	return s.syncDirectory()
}

func (s *ResultStore) syncDirectory() error {
	dir, err := s.root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(syncClaimDirectory(dir), dir.Close())
}

// Delete discards a finalized result. Active reservations and readers can
// finish first. Calling Delete again after a successful deletion is safe.
func (s *ResultStore) Delete(operationID string, apiKeyID int64) error {
	if !operationPattern.MatchString(operationID) || apiKeyID <= 0 {
		return ErrResultNotFound
	}
	name := resultFileName(operationID, apiKeyID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.fault != nil {
		return ErrResultStorage
	}
	if s.pending[name] != 0 {
		return ErrResultBusy
	}
	if s.entries[name] == nil {
		return nil
	}
	if err := s.removeResultLocked(name); err != nil {
		return err
	}
	return s.syncRemovalLocked()
}

func (s *ResultStore) removeResultLocked(name string) error {
	entry := s.entries[name]
	if entry.readers != 0 {
		return ErrResultBusy
	}
	if err := s.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(ErrResultStorage, err)
	}
	delete(s.entries, name)
	s.bytes -= entry.size
	return nil
}

func (s *ResultStore) syncRemovalLocked() error {
	if err := s.syncDirectory(); err != nil {
		s.fault = err
		return errors.Join(ErrResultStorage, err)
	}
	return nil
}

// Prune drops expired results with no active readers. Open rejects expired
// results even when maintenance has yet to reclaim their disk space.
func (s *ResultStore) Prune(now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.fault != nil {
		return 0, ErrResultStorage
	}
	removed := 0
	var lastErr error
	for name, entry := range s.entries {
		if now.Before(entry.expiresAt) || entry.readers != 0 {
			continue
		}
		if err := s.removeResultLocked(name); err != nil {
			lastErr = err
			break
		}
		removed++
	}
	if removed > 0 {
		lastErr = errors.Join(lastErr, s.syncRemovalLocked())
	}
	return removed, lastErr
}
