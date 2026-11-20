package imagerelay

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ClaimRetention keeps operations across restarts well beyond the signing
// window. It bounds the state of the single-instance main site. Future durable
// reconciliation still needs an operation ledger independent of this guard.
const ClaimRetention = time.Hour

var (
	ErrAlreadyClaimed = errors.New("image relay operation already claimed")
	ErrClaimStorage   = errors.New("image relay claim storage unavailable")
)

// ClaimStore holds pre-dispatch claims in the main site's persistent data
// directory. The application user must exclusively control this directory.
// Keep one store per directory and run maintenance from the owning instance.
type ClaimStore struct {
	dir string
	mu  sync.Mutex
}

func NewClaimStore(privateDir string) (*ClaimStore, error) {
	if !filepath.IsAbs(privateDir) || filepath.Clean(privateDir) != privateDir {
		return nil, ErrClaimStorage
	}
	if err := os.MkdirAll(privateDir, 0700); err != nil {
		return nil, errors.Join(ErrClaimStorage, err)
	}
	store := &ClaimStore{dir: privateDir}
	root, err := store.openRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// Persist the claim directory entry as well as later claim entries.
	parent, err := os.Open(filepath.Dir(privateDir))
	if err != nil {
		return nil, errors.Join(ErrClaimStorage, err)
	}
	defer parent.Close()
	if err := syncClaimDirectory(parent); err != nil {
		return nil, errors.Join(ErrClaimStorage, err)
	}
	return store, nil
}

func (s *ClaimStore) openRoot() (*os.Root, error) {
	if s == nil {
		return nil, ErrClaimStorage
	}
	info, err := os.Lstat(s.dir)
	if err != nil {
		return nil, errors.Join(ErrClaimStorage, err)
	}
	// Windows mode bits do not represent access control lists.
	if !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0700) {
		return nil, ErrClaimStorage
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return nil, errors.Join(ErrClaimStorage, err)
	}
	return root, nil
}

// Claim creates an exclusive record for a verified request before any paid
// dispatch. The operation stays claimed even if the process crashes or an
// error occurs after creating its record. Never resubmit an uncertain
// operation with a fresh signature after its claim expires.
func (s *ClaimStore) Claim(req Request, now time.Time) error {
	if !validBinding(req.Binding) {
		return ErrInvalidBinding
	}
	if !fresh(req.IssuedAt, now, MaxRequestAge) {
		return ErrExpired
	}
	if s == nil {
		return ErrClaimStorage
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	// Ignore the route, key number and body in the storage key. Reusing the
	// same operation with changes to them must still conflict.
	name := claimName(req.Binding)
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, fs.ErrExist) {
		return ErrAlreadyClaimed
	}
	if err != nil {
		return errors.Join(ErrClaimStorage, err)
	}
	writeErr := json.NewEncoder(file).Encode(req)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return errors.Join(ErrClaimStorage, err)
	}
	return syncClaimRoot(root)
}

func claimName(b Binding) string {
	scope := strings.ToLower(b.Source) + "\x00" + strings.ToLower(b.Destination) + "\x00" + b.OperationID
	return SHA256Hex([]byte(scope)) + ".claim"
}

func syncClaimDirectory(dir *os.File) error {
	// Directory fsync is supported on the Linux production filesystem.
	if runtime.GOOS == "windows" {
		return nil
	}
	return dir.Sync()
}

func syncClaimRoot(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return errors.Join(ErrClaimStorage, err)
	}
	if err := errors.Join(syncClaimDirectory(dir), dir.Close()); err != nil {
		return errors.Join(ErrClaimStorage, err)
	}
	return nil
}

// Prune deletes only old regular claim files. Schedule it periodically outside
// request handling. Fresh signatures expire well before their records do.
func (s *ClaimStore) Prune(now time.Time) (int, error) {
	if s == nil {
		return 0, ErrClaimStorage
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.openRoot()
	if err != nil {
		return 0, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return 0, errors.Join(ErrClaimStorage, err)
	}
	defer dir.Close()

	removed := 0
	cutoff := now.Add(-ClaimRetention)
	for {
		entries, scanErr := dir.ReadDir(128)
		for _, entry := range entries {
			name := entry.Name()
			prefix, isClaim := strings.CutSuffix(name, ".claim")
			if !isClaim || !digestPattern.MatchString(prefix) || !entry.Type().IsRegular() {
				continue
			}
			info, err := entry.Info()
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return removed, errors.Join(ErrClaimStorage, err)
			}
			if !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
				continue
			}
			if err := root.Remove(name); errors.Is(err, fs.ErrNotExist) {
				continue
			} else if err != nil {
				return removed, errors.Join(ErrClaimStorage, err)
			}
			removed++
		}
		if errors.Is(scanErr, io.EOF) {
			break
		}
		if scanErr != nil {
			return removed, fmt.Errorf("%w: directory scan: %v", ErrClaimStorage, scanErr)
		}
	}
	if removed > 0 {
		if err := syncClaimDirectory(dir); err != nil {
			return removed, errors.Join(ErrClaimStorage, err)
		}
	}
	return removed, nil
}
