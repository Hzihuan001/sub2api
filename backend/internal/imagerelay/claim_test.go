package imagerelay

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClaimPersistsAndIsExclusiveAcrossInstances(t *testing.T) {
	base := filepath.Join(t.TempDir(), "claims")
	first, err := NewClaimStore(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewClaimStore(base)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Binding: fixtureBinding(), IssuedAt: fixtureNow.Unix()}
	var won atomic.Int64
	var wg sync.WaitGroup
	for i := range 16 {
		store := []*ClaimStore{first, second}[i%2]
		wg.Go(func() {
			err := store.Claim(request, fixtureNow)
			switch {
			case err == nil:
				won.Add(1)
			case !errors.Is(err, ErrAlreadyClaimed):
				t.Errorf("unexpected claim error: %v", err)
			}
		})
	}
	wg.Wait()
	if won.Load() != 1 {
		t.Fatalf("count of authorized dispatches: %d", won.Load())
	}
	restarted, err := NewClaimStore(base)
	if err != nil {
		t.Fatal(err)
	}
	request.BodySHA256 = SHA256Hex([]byte("another request body"))
	request.MainAPIKeyID++
	if err := restarted.Claim(request, fixtureNow.Add(time.Second)); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("same operation survived restart with different key/body: %v", err)
	}
	request.Source = "separate-site"
	if err := restarted.Claim(request, fixtureNow.Add(time.Second)); err != nil {
		t.Fatalf("separate peer's new operation rejected: %v", err)
	}
}

func TestPruneKeepsCurrentClaimsAndNeverReadmitsStaleSignature(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "claims")
	store, err := NewClaimStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	stale := Request{Binding: fixtureBinding(), IssuedAt: now.Add(-2 * ClaimRetention).Unix()}
	if err := store.Claim(stale, now.Add(-2*ClaimRetention)); err != nil {
		t.Fatal(err)
	}
	staleFile := filepath.Join(dir, claimName(stale.Binding))
	if err := os.Chtimes(staleFile, now.Add(-2*ClaimRetention), now.Add(-2*ClaimRetention)); err != nil {
		t.Fatal(err)
	}
	current := Request{Binding: fixtureBinding(), IssuedAt: now.Unix()}
	current.OperationID = "1123456789abcdef0123456789abcdef"
	if err := store.Claim(current, now); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dir, "unrelated.txt")
	if err := os.WriteFile(unrelated, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(unrelated, now.Add(-2*ClaimRetention), now.Add(-2*ClaimRetention)); err != nil {
		t.Fatal(err)
	}
	n, err := store.Prune(now)
	if err != nil || n != 1 {
		t.Fatalf("Prune = %d, %v", n, err)
	}
	if _, err := os.Stat(staleFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale claim remains, %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated entry removed: %v", err)
	}
	if err := store.Claim(current, now); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("pruned current claim: %v", err)
	}
	if err := store.Claim(stale, now); !errors.Is(err, ErrExpired) {
		t.Fatalf("replay of stale signature after prune: %v", err)
	}
}

func TestClaimEmptyRecordFailsClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "claims")
	store, err := NewClaimStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Binding: fixtureBinding(), IssuedAt: fixtureNow.Unix()}
	// Model a process that died immediately after exclusive creation.
	if err := os.WriteFile(filepath.Join(dir, claimName(request.Binding)), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Claim(request, fixtureNow); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("empty claim allowed new dispatch: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := NewClaimStore(dir); !errors.Is(err, ErrClaimStorage) {
			t.Fatalf("accepted non-private directory: %v", err)
		}
	}
}
