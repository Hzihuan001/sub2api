//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/require"
)

func sharedBatchImageTestReferences() []BatchImageReferenceInput {
	refs := make([]BatchImageReferenceInput, 16)
	for i := range refs {
		refs[i] = BatchImageReferenceInput{
			ID:       fmt.Sprintf("reference-%02d.png", 16-i),
			MimeType: "image/png",
			Data:     []byte(fmt.Sprintf("unique-reference-bytes-%02d", i)),
		}
	}
	return refs
}

func TestBatchImageSharedSixteenReferencesSurviveExpansionAndEdits(t *testing.T) {
	svc, repo, _, _, _ := newTestBatchImagePublicService(true)
	managed := configureTestAppManagedBatchService(svc)
	refs := sharedBatchImageTestReferences()
	req := validBatchImageSubmitRequest()
	req.Provider, req.Model = BatchImageProviderAppManaged, "gpt-image-2.5-sunburst"
	// Upload shared bytes once; every output must still receive all 16 images
	// through the ordinary single-image edits endpoint after repeat expansion.
	req.SharedReferenceImages = refs
	req.Items = []BatchImageSubmitItem{
		{CustomID: "front", Prompt: "front", OutputCount: 2},
		{CustomID: "side", Prompt: "side", OutputCount: 3},
	}
	got, err := svc.Submit(context.Background(), testBatchImageOwner(), req, "")
	require.NoError(t, err)
	require.Equal(t, 5, got.ItemCount)
	require.InDelta(t, 1.25, got.EstimatedCost, 1e-12)
	require.Len(t, managed.submits, 1)
	require.Len(t, managed.submits[0].Items, 5)
	require.Len(t, repo.items[got.ID], 5)

	upstream := &appManagedTestUpstream{}
	provider := NewAppManagedBatchImageProvider(AppManagedBatchImageProviderOptions{HTTPUpstream: upstream}).(*appManagedBatchImageProvider)
	account := &Account{ID: 7, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Credentials: map[string]any{
		"api_key": "test-key", "base_url": "https://upstream.invalid",
	}}
	wantPrompts := map[string]string{
		"front_01": "front", "front_02": "front",
		"side_01": "side", "side_02": "side", "side_03": "side",
	}
	for _, stored := range repo.items[got.ID] {
		var payload managedBatchImageItemPayload
		require.NoError(t, json.Unmarshal(stored.InputPayload, &payload))
		require.Equal(t, wantPrompts[stored.CustomID], payload.Item.Prompt)
		require.Equal(t, refs, payload.Item.ReferenceImages)
		require.Zero(t, payload.Item.OutputCount)
		line, err := provider.generateOne(context.Background(), repo.jobs[got.ID], account, payload.Item, payload.ImageSize, payload.AspectRatio, payload.ResponseMimeType)
		require.NoError(t, err)
		result, err := ParseBatchImageResultLine(line, 0)
		require.NoError(t, err)
		require.Equal(t, BatchImageParsedStatusSucceeded, result.Status)
		require.Equal(t, "https://upstream.invalid/v1/images/edits", upstream.lastURL)

		mediaType, params, err := mime.ParseMediaType(upstream.lastContentType)
		require.NoError(t, err)
		require.Equal(t, "multipart/form-data", mediaType)
		reader := multipart.NewReader(bytes.NewReader(upstream.lastBody), params["boundary"])
		fields := map[string]string{}
		imageIndex := 0
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			data, err := io.ReadAll(part)
			require.NoError(t, err)
			if part.FileName() == "" {
				fields[part.FormName()] = string(data)
				continue
			}
			require.Less(t, imageIndex, len(refs))
			require.Equal(t, "image[]", part.FormName())
			require.Equal(t, refs[imageIndex].ID, part.FileName())
			require.Equal(t, refs[imageIndex].Data, data)
			imageIndex++
		}
		require.Equal(t, 16, imageIndex)
		require.Equal(t, payload.Item.Prompt, fields["prompt"])
		require.Equal(t, req.Model, fields["model"])
		require.Equal(t, "2048x2048", fields["size"])
		delete(wantPrompts, stored.CustomID)
	}
	require.Empty(t, wantPrompts)
	require.Equal(t, 5, upstream.calls)
}

func TestBatchImageSharedReferencesCanonicalizeLikeLegacyItems(t *testing.T) {
	svc, _, _, _, _ := newTestBatchImagePublicService(true)
	refs := sharedBatchImageTestReferences()
	legacy := validBatchImageSubmitRequest()
	legacy.Model, legacy.Provider = "gpt-image-2.5-sunburst", BatchImageProviderAppManaged
	legacy.TaskName = "same-task"
	legacy.Items[0].ReferenceImages, legacy.Items[1].ReferenceImages = refs, refs
	shared := legacy
	shared.SharedReferenceImages = refs
	shared.Items = []BatchImageSubmitItem{{CustomID: "cover_001", Prompt: "hero"}, {CustomID: "cover_002", Prompt: "clean"}}
	oldJSON, err := json.Marshal(legacy)
	require.NoError(t, err)
	sharedJSON, err := json.Marshal(shared)
	require.NoError(t, err)
	require.Equal(t, 2, bytes.Count(oldJSON, []byte(`"reference-16.png"`)))
	require.Equal(t, 1, bytes.Count(sharedJSON, []byte(`"reference-16.png"`)))
	oldNormalized, err := svc.validateSubmitRequest(legacy)
	require.NoError(t, err)
	newNormalized, err := svc.validateSubmitRequest(shared)
	require.NoError(t, err)
	require.Nil(t, newNormalized.SharedReferenceImages)
	require.Equal(t, HashBatchImageSubmitRequest(oldNormalized), HashBatchImageSubmitRequest(newNormalized))
}

func TestBatchImageEmptySharedReferencesClearPerItemReferences(t *testing.T) {
	svc, _, _, _, _ := newTestBatchImagePublicService(true)
	req := validBatchImageSubmitRequest()
	req.Model, req.Provider = "gpt-image-2.5-sunburst", BatchImageProviderAppManaged
	req.SharedReferenceImages = []BatchImageReferenceInput{}
	req.Items[0].ReferenceImages = sharedBatchImageTestReferences()
	normalized, err := svc.validateSubmitRequest(req)
	require.NoError(t, err)
	for _, item := range normalized.Items {
		require.Empty(t, item.ReferenceImages)
	}
}

func TestBatchImageSharedReferencesKeepExpandedJobLimits(t *testing.T) {
	t.Run("sixteen per prompt still counts every repeated attachment", func(t *testing.T) {
		svc, _, _, _, _ := newTestBatchImagePublicService(true)
		svc.Config.BatchImage.MaxItemsPerJobDefault = 16
		require.Equal(t, 1000, svc.maxReferenceImagesPerJob())
		refs := sharedBatchImageTestReferences()
		req := validBatchImageSubmitRequest()
		req.Provider, req.Model = BatchImageProviderAppManaged, "gpt-image-2.5-sunburst"
		req.SharedReferenceImages = refs
		req.Items = nil
		for i := 0; i < 16; i++ {
			req.Items = append(req.Items, BatchImageSubmitItem{CustomID: fmt.Sprintf("item_%d", i), Prompt: "cat", OutputCount: 4})
		}
		req.Items[15].OutputCount = 2 // 62 outputs x16 references = 992.
		normalized, err := svc.validateSubmitRequest(req)
		require.NoError(t, err)
		require.Len(t, normalized.Items, 62)
		req.Items[15].OutputCount = 3 // 63 outputs x16 references = 1008.
		_, err = svc.validateSubmitRequest(req)
		require.ErrorIs(t, err, ErrBatchImageTooManyReferenceImages)
	})

	t.Run("shared inline bytes are counted again for every output", func(t *testing.T) {
		svc, _, _, _, _ := newTestBatchImagePublicService(true)
		svc.Config.BatchImage.MaxItemsPerJobDefault = 5
		require.Equal(t, 128*1024*1024, svc.maxReferenceInlineBytesPerJob())
		refs := sharedBatchImageTestReferences()
		for i := range refs {
			refs[i].Data = make([]byte, 512*1024)
		}
		req := validBatchImageSubmitRequest()
		req.Provider, req.Model = BatchImageProviderAppManaged, "gpt-image-2.5-sunburst"
		req.SharedReferenceImages = refs
		req.Items = nil
		for i := 0; i < 4; i++ {
			req.Items = append(req.Items, BatchImageSubmitItem{CustomID: fmt.Sprintf("item_%d", i), Prompt: "cat", OutputCount: 4})
		}
		_, err := svc.validateSubmitRequest(req) // 16 outputs x8MiB = 128MiB.
		require.NoError(t, err)
		req.Items = append(req.Items, BatchImageSubmitItem{CustomID: "over", Prompt: "cat"})
		_, err = svc.validateSubmitRequest(req)
		require.ErrorIs(t, err, ErrBatchImageReferenceImagesTooLarge)
	})
}
