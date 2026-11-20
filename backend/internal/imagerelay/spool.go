package imagerelay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
)

var (
	ErrSpoolCapacity  = errors.New("image relay spool capacity exhausted")
	ErrSpoolIntegrity = errors.New("image relay spooled result failed integrity checks")
)

const spoolBufferBytes = 64 << 10
const spoolLockName = ".image-relay.lock"

var spoolFileName = regexp.MustCompile("^image-relay-[0-9a-f]{32}[.]tmp$")

// Spool reserves temporary disk space before a relay request is dispatched.
// Keep its private directory separate from durable results and give each
// configured instance its own directory.
type Spool struct {
	root         *os.Root
	lock         *os.File
	maxFiles     int
	maxBytes     int64
	minFreeBytes int64

	mu      sync.Mutex
	files   int
	bytes   int64
	pending int64
	closed  bool
	fault   error
}

// NewSpool takes exclusive ownership of a dedicated directory. On Linux and
// Windows it removes temporary files left behind by a previous spool process.
// A zero minFreeBytes disables the filesystem free-space check.
func NewSpool(path string, maxFiles int, maxBytes, minFreeBytes int64) (*Spool, error) {
	if path == "" || maxFiles <= 0 || maxBytes <= 0 || minFreeBytes < 0 {
		return nil, errors.New("invalid image relay spool configuration")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0700) {
		return nil, errors.New("image relay spool requires a private directory with mode 0700")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	bound, err := root.Stat(".")
	if err != nil || !os.SameFile(info, bound) {
		root.Close()
		return nil, errors.New("image relay spool directory changed while opening")
	}
	lock, err := lockSpool(root)
	if err != nil {
		root.Close()
		return nil, err
	}
	s := &Spool{root: root, lock: lock, maxFiles: maxFiles, maxBytes: maxBytes, minFreeBytes: minFreeBytes}
	if err := s.removeStaleFiles(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Spool) removeStaleFiles() error {
	dir, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	var stale []string
	for {
		entries, readErr := dir.ReadDir(32)
		for _, entry := range entries {
			if entry.Name() == spoolLockName {
				continue
			}
			if !spoolFileName.MatchString(entry.Name()) || !entry.Type().IsRegular() {
				return errors.New("image relay spool directory contains an unexpected entry")
			}
			stale = append(stale, entry.Name())
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	for _, name := range stale {
		if err := s.root.Remove(name); err != nil {
			return err
		}
	}
	return nil
}

// Close releases directory ownership once all reservations and files have
// been closed. A busy spool stays open so current callers can complete.
func (s *Spool) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if s.files != 0 {
		return errors.New("image relay spool still has active reservations")
	}
	s.closed = true
	return errors.Join(unlockSpool(s.root, s.lock), s.root.Close())
}

// Reserve holds one result slot and budgets its maximum response size before
// contacting the upstream. Closing the reservation releases both limits.
func (s *Spool) Reserve(maxExpectedBytes int64) (*Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.fault != nil || maxExpectedBytes <= 0 ||
		s.files >= s.maxFiles || maxExpectedBytes > s.maxBytes-s.bytes {
		return nil, ErrSpoolCapacity
	}
	if s.minFreeBytes > 0 {
		free, err := spoolFreeBytes(s.root)
		if err != nil {
			return nil, fmt.Errorf("%w: check available disk space: %v", ErrSpoolCapacity, err)
		}
		if free < uint64(s.minFreeBytes) || uint64(s.pending+maxExpectedBytes) > free-uint64(s.minFreeBytes) {
			return nil, ErrSpoolCapacity
		}
	}
	s.files++
	s.bytes += maxExpectedBytes
	s.pending += maxExpectedBytes
	return &Reservation{spool: s, budget: maxExpectedBytes}, nil
}

// Reservation is a single attempt. A successful Receive transfers ownership
// to the result file, which holds the quota until it is closed.
type Reservation struct {
	spool  *Spool
	budget int64

	mu       sync.Mutex
	finished bool
}

func (r *Reservation) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.release()
	return nil
}

func (r *Reservation) release() {
	if r.finished {
		return
	}
	r.finished = true
	s := r.spool
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files--
	s.bytes -= r.budget
	s.pending -= r.budget
}

// Receive checks the declared byte length and lowercase SHA-256 digest
// while copying to a private file. Reads from a closable upstream are
// interrupted when the receive context is canceled.
func (r *Reservation) Receive(ctx context.Context, src io.Reader, expectedBytes int64, expectedSHA256 string) (*SpoolFile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return nil, ErrSpoolCapacity
	}
	defer r.release()
	want, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(want) != sha256.Size || hex.EncodeToString(want) != expectedSHA256 ||
		src == nil || expectedBytes <= 0 || expectedBytes > r.budget {
		return nil, ErrSpoolIntegrity
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if closer, ok := src.(io.ReadCloser); ok {
		stop := context.AfterFunc(ctx, func() { closer.Close() })
		defer stop()
	}

	s := r.spool
	nonce := make([]byte, 16)
	_, err = rand.Read(nonce)
	if err != nil {
		return nil, err
	}
	name := "image-relay-" + hex.EncodeToString(nonce) + ".tmp"
	file, err := s.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !r.finished {
			s.removeFile(name, file)
		}
	}()
	hash := sha256.New()
	input := spoolContextReader{ctx, src}
	n, err := io.CopyBuffer(io.MultiWriter(file, hash), io.LimitReader(input, expectedBytes), make([]byte, spoolBufferBytes))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if n != expectedBytes || hex.EncodeToString(hash.Sum(nil)) != expectedSHA256 {
		return nil, ErrSpoolIntegrity
	}
	var excess [1]byte
	if _, err := io.ReadFull(input, excess[:]); err != io.EOF {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, err
		}
		return nil, ErrSpoolIntegrity
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.bytes -= r.budget - expectedBytes
	s.pending -= r.budget
	s.mu.Unlock()
	r.finished = true
	return &SpoolFile{File: file, spool: s, name: name, length: expectedBytes}, nil
}

type spoolContextReader struct {
	ctx context.Context
	src io.Reader
}

func (r spoolContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.src.Read(p)
}

// SpoolFile is positioned at the start of the verified body. Close deletes
// the file and releases the result's quota.
type SpoolFile struct {
	*os.File
	spool  *Spool
	name   string
	length int64
	once   sync.Once
	err    error
}

func (f *SpoolFile) Close() error {
	f.once.Do(func() {
		f.err = f.spool.removeFile(f.name, f.File)
		s := f.spool
		s.mu.Lock()
		defer s.mu.Unlock()
		s.files--
		s.bytes -= f.length
	})
	return f.err
}

func (s *Spool) removeFile(name string, file *os.File) error {
	closeErr := file.Close()
	err := s.root.Remove(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.mu.Lock()
		s.fault = err
		s.mu.Unlock()
		return errors.Join(closeErr, err)
	}
	return closeErr
}

func spoolLockFile(root *os.Root, flags int) (*os.File, error) {
	if existing, err := root.Lstat(spoolLockName); err == nil {
		if !existing.Mode().IsRegular() {
			return nil, errors.New("invalid image relay spool lock file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := root.OpenFile(spoolLockName, os.O_RDWR|os.O_CREATE|flags, 0600)
	if err != nil {
		return nil, err
	}
	current, err := root.Lstat(spoolLockName)
	opened, statErr := file.Stat()
	if err != nil || statErr != nil || !current.Mode().IsRegular() || !os.SameFile(current, opened) {
		file.Close()
		return nil, errors.New("image relay spool lock file changed while opening")
	}
	return file, nil
}
