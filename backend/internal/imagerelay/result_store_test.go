package imagerelay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const firstResultID = "98c33768c9184e10a0107a32068c357e"
const secondResultID = "9a56eed59b36c22b33157a3d9aa8535a"

func testResultMetadata(body []byte) ResultMetadata {
	return ResultMetadata{Status: http.StatusOK, ContentType: "application/json", BodyBytes: int64(len(body)), BodySHA256: SHA256Hex(body),
		Proof: []byte(`{"image_count":1}`)}
}

func TestResultStoreRestartsWithOwnerAndExactBody(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "results")
	const owner int64 = 42
	body := []byte(`{"data":[{"b64_json":"ESIz"}]}`)
	store, err := NewResultStore(dir, 2, 1<<20, 0, time.Hour)
	require.NoError(t, err)
	reservation, err := store.Reserve(firstResultID, owner, 4096)
	require.NoError(t, err)
	_, err = store.Reserve(firstResultID, owner, 4096)
	require.ErrorIs(t, err, ErrResultExists)
	_, err = NewResultStore(dir, 2, 1<<20, 0, time.Hour)
	require.ErrorIs(t, err, ErrResultStorage)
	require.NoError(t, reservation.Save(ctx, bytes.NewReader(body), testResultMetadata(body)))
	require.NoError(t, reservation.Close())
	require.NoError(t, store.Close())

	staging := filepath.Join(dir, resultFileName(secondResultID, owner)+".part")
	require.NoError(t, os.WriteFile(staging, body[:3], 0600))
	store, err = NewResultStore(dir, 2, 1<<20, 0, time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	_, err = os.Stat(staging)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = store.Open(ctx, firstResultID, owner+1)
	require.ErrorIs(t, err, ErrResultNotFound)
	_, err = store.Open(ctx, "../"+firstResultID, owner)
	require.ErrorIs(t, err, ErrResultNotFound)
	_, err = store.Open(ctx, secondResultID, owner)
	require.ErrorIs(t, err, ErrResultNotFound)

	result, err := store.Open(ctx, firstResultID, owner)
	require.NoError(t, err)
	defer result.Close()
	require.Equal(t, testResultMetadata(body).BodySHA256, result.Metadata.BodySHA256)
	require.Equal(t, testResultMetadata(body).BodyBytes, result.Metadata.BodyBytes)
	require.JSONEq(t, `{"image_count":1}`, string(result.Metadata.Proof))
	require.Equal(t, time.Hour, result.Metadata.ExpiresAt.Sub(result.Metadata.CreatedAt))
	data, err := io.ReadAll(result)
	require.NoError(t, err)
	require.Equal(t, body, data)
	require.ErrorIs(t, store.Delete(firstResultID, owner), ErrResultBusy)
	require.ErrorIs(t, store.Close(), ErrResultBusy)
	require.NoError(t, result.Close())
	require.NoError(t, store.Delete(firstResultID, owner))
	require.NoError(t, store.Delete(firstResultID, owner))
	_, err = store.Open(ctx, firstResultID, owner)
	require.ErrorIs(t, err, ErrResultNotFound)
}

func TestResultStoreReleasesFailedReservationsAndPrunes(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "results")
	const owner int64 = 42
	limit := resultMaxOverhead + 8192
	store, err := NewResultStore(dir, 2, limit, 0, time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	first, err := store.Reserve(firstResultID, owner, 4096)
	require.NoError(t, err)
	_, err = store.Reserve(secondResultID, owner, 4096)
	require.ErrorIs(t, err, ErrResultCapacity)
	body := []byte(`{"data":[]}`)
	metadata := testResultMetadata(body)
	require.ErrorIs(t, first.Save(ctx, bytes.NewReader(body[:3]), metadata), ErrResultIntegrity)
	_, err = store.Open(ctx, firstResultID, owner)
	require.ErrorIs(t, err, ErrResultNotFound)

	first, err = store.Reserve(firstResultID, owner, 4096)
	require.NoError(t, err)
	require.ErrorIs(t, first.Save(ctx, strings.NewReader(string(body)+"x"), metadata), ErrResultIntegrity)
	first, err = store.Reserve(firstResultID, owner, 4096)
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, first.Save(canceled, bytes.NewReader(body), metadata), context.Canceled)
	first, err = store.Reserve(firstResultID, owner, 4096)
	require.NoError(t, err)
	require.NoError(t, first.Save(ctx, bytes.NewReader(body), metadata))

	second, err := store.Reserve(secondResultID, owner, 4096)
	require.NoError(t, err)
	require.NoError(t, second.Save(ctx, bytes.NewReader(body), metadata))
	_, err = store.Reserve("fac646700a45ec02731d33528dbca20c", owner, 1024)
	require.ErrorIs(t, err, ErrResultCapacity)
	reader, err := store.Open(ctx, firstResultID, owner)
	require.NoError(t, err)
	defer reader.Close()
	pruned, err := store.Prune(reader.Metadata.ExpiresAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, 1, pruned)
	require.NoError(t, reader.Close())
	pruned, err = store.Prune(reader.Metadata.ExpiresAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, 1, pruned)
	_, err = store.Open(ctx, firstResultID, owner)
	require.ErrorIs(t, err, ErrResultNotFound)
}

func TestResultStoreRejectsTamperedBodyAfterRestart(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "results")
	const owner int64 = 42
	body := []byte(`{"data":[{"b64_json":"ESIz"}]}`)
	store, err := NewResultStore(dir, 2, 1<<20, 0, time.Hour)
	require.NoError(t, err)
	for _, id := range []string{firstResultID, secondResultID} {
		reservation, err := store.Reserve(id, owner, 4096)
		require.NoError(t, err)
		require.NoError(t, reservation.Save(ctx, bytes.NewReader(body), testResultMetadata(body)))
	}
	require.NoError(t, store.Close())
	path := filepath.Join(dir, resultFileName(firstResultID, owner))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	require.NoError(t, err)
	fileInfo, err := file.Stat()
	require.NoError(t, err)
	require.NoError(t, file.Close())
	file, err = os.OpenFile(path, os.O_WRONLY, 0600)
	require.NoError(t, err)
	_, err = file.WriteAt([]byte{'?'}, fileInfo.Size()-3)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	store, err = NewResultStore(dir, 2, 1<<20, 0, time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	_, err = store.Open(ctx, firstResultID, owner)
	require.True(t, errors.Is(err, ErrResultIntegrity), "error = %v", err)
	second, err := store.Open(ctx, secondResultID, owner)
	require.NoError(t, err)
	defer second.Close()
	data, err := io.ReadAll(second)
	require.NoError(t, err)
	require.Equal(t, body, data)
}
