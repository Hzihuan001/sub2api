package service

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	batchImageMinDimension = 256
	batchImageMaxDimension = 3840
	batchImageMaxPixelArea = 3840 * 2160
	batchImageSeedreamMinPixelArea = 921_600
	// Seedream 5.0 Pro exposes the 2K/custom contract through compatible
	// adapters; keep the API-side limit aligned with the workbench so callers
	// cannot bypass it by submitting directly to the batch endpoint.
	batchImageSeedreamMaxPixelArea = 4_624_220
)

// normalizeBatchImageSize validates the public batch image size contract and
// returns a stable representation for persistence and upstream forwarding.
// Tier values remain uppercase while custom dimensions use a lowercase "x".
func normalizeBatchImageSize(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	switch strings.ToUpper(trimmed) {
	case ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K:
		return strings.ToUpper(trimmed), true
	}

	parts := strings.Split(strings.ToLower(trimmed), "x")
	if len(parts) != 2 {
		return "", false
	}
	width, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return "", false
	}
	height, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return "", false
	}
	if width < batchImageMinDimension || height < batchImageMinDimension ||
		width > batchImageMaxDimension || height > batchImageMaxDimension {
		return "", false
	}
	if width%16 != 0 || height%16 != 0 || width*height > batchImageMaxPixelArea {
		return "", false
	}
	// Avoid floating point rounding at the supported aspect-ratio boundaries.
	if width*3 < height || height*3 < width {
		return "", false
	}
	return fmt.Sprintf("%dx%d", width, height), true
}

// isBatchImageSizeSupportedByModel applies model-specific restrictions after
// account model_mapping has been resolved. Generic size validation above is
// intentionally provider-agnostic because GPT-image supports the full 4K
// contract while Seedream 5.0 Pro does not.
func isBatchImageSizeSupportedByModel(model, size string) bool {
	if !isSeedreamImageGenerationModel(model) {
		return true
	}
	normalized := strings.ToUpper(strings.TrimSpace(size))
	if normalized == ImageBillingSize4K {
		return false
	}
	if !strings.Contains(normalized, "X") {
		return true
	}
	parts := strings.Split(normalized, "X")
	if len(parts) != 2 {
		return false
	}
	width, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || width <= 0 || height <= 0 {
		return false
	}
	pixelArea := width * height
	return pixelArea >= batchImageSeedreamMinPixelArea && pixelArea <= batchImageSeedreamMaxPixelArea
}
