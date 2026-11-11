//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchImagePublicService_ListModelsAdvertisesManagedImageModels(t *testing.T) {
	svc, _, _, _, _ := newTestBatchImagePublicService(true)
	provider := configureTestAppManagedBatchService(svc)
	accountRepo := svc.AccountRepo.(*publicBatchImageAccountRepo)
	accountRepo.accounts[0].Credentials["model_mapping"] = map[string]any{
		"gpt-image-2.6-flare":          "gpt-image-2.6-flare",
		"gpt-image-2.6-sunburst":       "gpt-image-2.6-sunburst",
		"grok-imagine-image-2.0":       "grok-imagine-image-2.0",
		"grok-imagine-image-quality":   "grok-imagine-image-quality",
	}

	got, err := svc.ListModels(context.Background(), testBatchImageOwner())
	require.NoError(t, err)
	require.NotNil(t, provider)

	models := make(map[string]BatchImagePublicModel, len(got.Data))
	for _, model := range got.Data {
		models[model.ID] = model
	}
	for _, expected := range []struct {
		id    string
		refs  int
	}{
		{id: "gpt-image-2.6-flare", refs: 16},
		{id: "gpt-image-2.6-sunburst", refs: 16},
		{id: "grok-imagine-image-2.0", refs: 5},
		{id: "grok-imagine-image-quality", refs: 5},
	} {
		model, ok := models[expected.id]
		require.Truef(t, ok, "model %s missing from catalog", expected.id)
		require.Equal(t, BatchImageProviderAppManaged, model.Provider, expected.id)
		require.Equal(t, "managed_fanout", model.Mode, expected.id)
		require.Equal(t, expected.refs, model.ReferenceImageLimit, expected.id)
		require.True(t, model.SupportsSharedReferenceImages, expected.id)
	}
}
