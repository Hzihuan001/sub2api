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
	// The snapshot account carries no operator mapping, so it must not drag the
	// default catalogue in behind it: it only ever observed the two ids above.
	for _, id := range cursorpkg.DefaultModelIDs() {
		require.NotContains(t, models, id,
			"account 2 observed a snapshot; the default catalogue must not be unioned in")
	}
}

// A free-tier account is only served "auto"/default upstream. Before the
// snapshot became authoritative it still advertised the whole catalogue, and
// every request for one of those models failed at the agent turn.
func TestGetAvailableModelsCursorSnapshotIsAuthoritative(t *testing.T) {
	repo := &cursorObservedModelsAccountRepoStub{
		accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformCursor,
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					cursorObservedModelsExtraKey: map[string]any{
						"models":     []any{"auto"},
						"fetched_at": time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	require.Equal(t, []string{"auto"}, svc.GetAvailableModels(context.Background(), nil, PlatformCursor))
}

// With a snapshot present an operator mapping filters rather than extends: an
// alias survives when its target was observed, and disappears when it was not.
func TestGetAvailableModelsCursorMappingFiltersOnSnapshot(t *testing.T) {
	repo := &cursorObservedModelsAccountRepoStub{
		accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformCursor,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{
					"model_mapping": map[string]any{
						"claude-sonnet-4-5": "claude-4.5-sonnet-max",
						"gpt-5-codex":       "gpt-5.2",
					},
				},
				Extra: map[string]any{
					cursorObservedModelsExtraKey: map[string]any{
						"models":     []any{"claude-4.5-sonnet"},
						"fetched_at": time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	models := svc.GetAvailableModels(context.Background(), nil, PlatformCursor)
	require.ElementsMatch(t, []string{"claude-4.5-sonnet", "claude-sonnet-4-5"}, models)
}

func TestCursorObservedLookupKeyFoldsVariants(t *testing.T) {
	require.Equal(t, "claude-4.5-sonnet", cursorObservedLookupKey("claude-4.5-sonnet-max"))
	require.Equal(t, "claude-4.5-sonnet", cursorObservedLookupKey("Claude-4.5-Sonnet:max"))
	require.Equal(t, cursorpkg.AgentDefaultModel, cursorObservedLookupKey(" auto "))
	require.Equal(t, "gpt-5", cursorObservedLookupKey("gpt-5"))
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
