package imagerelay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func spoolDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func openTestSpool(t *testing.T, maxFiles int, maxBytes int64) (*Spool, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "result-spool")
	spool, err := NewSpool(path, maxFiles, maxBytes, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := spool.Close(); err != nil {
			t.Error(err)
		}
	})
	return spool, path
}

func TestSpoolTransferAndQuota(t *testing.T) {
	payload := bytes.Repeat([]byte("results"), 15000)
	size := int64(len(payload))
	spool, path := openTestSpool(t, 2, size*3)
	first, err := spool.Reserve(size * 2)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := spool.Reserve(size + 1); !errors.Is(err, ErrSpoolCapacity) {
		t.Fatalf("reserve unexpectedly exceeded byte limit: %v", err)
	}
	result, err := first.Receive(context.Background(), bytes.NewReader(payload), size, spoolDigest(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := spool.Reserve(size * 2)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := spool.Reserve(1); !errors.Is(err, ErrSpoolCapacity) {
		t.Fatalf("reserve unexpectedly exceeded file limit: %v", err)
	}
	if err := spool.Close(); err == nil {
		t.Fatal("closed a spool with active leases")
	}
	actual, err := io.ReadAll(result)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("incorrect verified result returned: %v", err)
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	last, err := spool.Reserve(size * 3)
	if err != nil {
		t.Fatalf("original capacity did not return: %v", err)
	}
	defer last.Close()
	assertNoSpoolFiles(t, path)
}

func TestSpoolRefusesIncompleteOrAlteredBody(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		digestOf string
	}{
		{"short", "ima", "image"},
		{"excess", "images", "image"},
		{"wrong digest", "other", "image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spool, path := openTestSpool(t, 1, 33)
			reservation, err := spool.Reserve(33)
			if err != nil {
				t.Fatal(err)
			}
			defer reservation.Close()
			out, err := reservation.Receive(context.Background(), bytes.NewBufferString(tc.body),
				int64(len(tc.digestOf)), spoolDigest([]byte(tc.digestOf)))
			if out != nil || !errors.Is(err, ErrSpoolIntegrity) {
				t.Fatalf("expected integrity failure, got %v, %v", out, err)
			}
			again, err := spool.Reserve(33)
			if err != nil {
				t.Fatalf("failed transfer did not release quota: %v", err)
			}
			defer again.Close()
			assertNoSpoolFiles(t, path)
		})
	}
}

func TestSpoolCancelClosesBlockedResponse(t *testing.T) {
	spool, path := openTestSpool(t, 1, 64)
	reservation, err := spool.Reserve(64)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Close()
	reader, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		file, err := reservation.Receive(ctx, reader, 6, spoolDigest([]byte("sample")))
		if file != nil {
			file.Close()
		}
		result <- err
	}()
	if _, err := io.WriteString(writer, "sam"); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocked response did not stop when canceled")
	}
	assertNoSpoolFiles(t, path)
}

func TestSpoolExclusiveOwnerAndStartupCleanup(t *testing.T) {
	spool, path := openTestSpool(t, 1, 64)
	if other, err := NewSpool(path, 1, 64, 0); err == nil {
		other.Close()
		t.Fatal("second process acquired the spool directory")
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(path, "image-relay-57c008aebf43b51f1df2d78892d6cdff.tmp")
	if err := os.WriteFile(name, []byte("abandoned response"), 0600); err != nil {
		t.Fatal(err)
	}
	stranger := filepath.Join(path, "keep-me.txt")
	if err := os.WriteFile(stranger, []byte("unrelated data"), 0600); err != nil {
		t.Fatal(err)
	}
	if other, err := NewSpool(path, 1, 64, 0); err == nil {
		other.Close()
		t.Fatal("unexpected file accepted in spool directory")
	}
	for _, file := range []string{stranger, name} {
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("invalid root was modified: %v", err)
		}
	}
	if err := os.Remove(stranger); err != nil {
		t.Fatal(err)
	}
	again, err := NewSpool(path, 1, 64, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	assertNoSpoolFiles(t, path)
}

func TestSpoolDiskFloor(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("disk availability checked on Windows and Linux")
	}
	path := filepath.Join(t.TempDir(), "result-spool")
	spool, err := NewSpool(path, 1, 64, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	if _, err := spool.Reserve(64); !errors.Is(err, ErrSpoolCapacity) {
		t.Fatalf("disk floor not enforced: %v", err)
	}
}

func assertNoSpoolFiles(t *testing.T, path string) {
	t.Helper()
	files, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() != spoolLockName {
			t.Fatalf("left temporary file %q", file.Name())
		}
	}
}
