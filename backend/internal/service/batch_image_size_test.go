//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeBatchImageSize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{name: "1K tier", in: "1k", want: "1K", ok: true},
		{name: "2K tier", in: " 2K ", want: "2K", ok: true},
		{name: "4K tier", in: "4k", want: "4K", ok: true},
		{name: "custom landscape", in: "3840X2160", want: "3840x2160", ok: true},
		{name: "custom portrait", in: "2160x3840", want: "2160x3840", ok: true},
		{name: "custom aligned", in: "1024x1536", want: "1024x1536", ok: true},
		{name: "too small", in: "240x256", ok: false},
		{name: "too large edge", in: "3856x2160", ok: false},
		{name: "not multiple of 16", in: "1025x1024", ok: false},
		{name: "area too large", in: "3840x3840", ok: false},
		{name: "ratio too wide", in: "3840x1200", ok: false},
		{name: "ratio too tall", in: "1200x3840", ok: false},
		{name: "tier-like unknown", in: "3K", ok: false},
		{name: "auto", in: "auto", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeBatchImageSize(tt.in)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBatchImageItemToPublicExposesRequestedImageSize(t *testing.T) {
	payload, err := json.Marshal(managedBatchImageItemPayload{ImageSize: "3840X2160"})
	require.NoError(t, err)

	got := BatchImageItemToPublic(&BatchImageItem{CustomID: "img_001", InputPayload: payload})
	require.Equal(t, "3840x2160", got.ImageSize)
}
