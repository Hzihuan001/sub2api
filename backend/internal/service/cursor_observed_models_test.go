//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func TestParseCursorObservedModelsRoundTrip(t *testing.T) {
	extra := map[string]any{
		cursorObservedModelsExtraKey: map[string]any{
			"models":     []any{"claude-4.5-sonnet", "gpt-5.2"},
			"fetched_at": time.Now().UTC().Format(time.RFC3339),
			"source":     "upstream_available_models",
		},
	}

	snap := parseCursorObservedModels(extra)
	require.NotNil(t, snap)
	require.Equal(t, []string{"claude-4.5-sonnet", "gpt-5.2"}, snap.Models)

	require.Equal(t, []string{"claude-4.5-sonnet", "gpt-5.2"}, CursorObservedModelIDs(extra))

	require.Nil(t, parseCursorObservedModels(nil))
	require.Nil(t, parseCursorObservedModels(map[string]any{}))
	require.Nil(t, parseCursorObservedModels(map[string]any{
		cursorObservedModelsExtraKey: map[string]any{"models": []any{}},
	}))
}

func TestCursorObservedModelIDsDeduplicates(t *testing.T) {
	ids := cursorObservedModelIDs([]cursorpkg.Model{
		{Name: "claude-4.5-sonnet"},
		{Name: " claude-4.5-sonnet "},
		{Name: ""},
		{Name: "gpt-5.2"},
	})
	require.Equal(t, []string{"claude-4.5-sonnet", "gpt-5.2"}, ids)
}

type cursorObservedModelsAccountRepoStub struct {
	rateLimitAccountRepoStub
	accounts []Account
}

func (r *cursorObservedModelsAccountRepoStub) ListSchedulable(_ context.Context) ([]Account, error) {
	return r.accounts, nil
}

func TestGetAvailableModelsMergesCursorObservedModels(t *testing.T) {
	repo := &cursorObservedModelsAccountRepoStub{
		accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformCursor,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"mapped-model": "upstream-model"},
				},
			},
			{
				ID:          2,
				Platform:    PlatformCursor,
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					cursorObservedModelsExtraKey: map[string]any{
						"models":     []any{"observed-model-a", "observed-model-b"},
						"fetched_at": time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	models := svc.GetAvailableModels(context.Background(), nil, PlatformCursor)
	// Explicit mapping keys and observed upstream models are merged in.
	require.Contains(t, models, "mapped-model")
	require.Contains(t, models, "observed-model-a")
	require.Contains(t, models, "observed-model-b")
	// Accounts without explicit mapping resolve the default identity mapping,
	// so the default catalog stays part of the union.
	for _, id := range cursorpkg.DefaultModelIDs() {
		require.Contains(t, models, id)
	}
}

func TestGetAvailableModelsCursorWithoutObservedUsesDefaultCatalog(t *testing.T) {
	repo := &cursorObservedModelsAccountRepoStub{
		accounts: []Account{
			{ID: 3, Platform: PlatformCursor, Status: StatusActive, Schedulable: true},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	models := svc.GetAvailableModels(context.Background(), nil, PlatformCursor)
	require.ElementsMatch(t, cursorpkg.DefaultModelIDs(), models)
}
