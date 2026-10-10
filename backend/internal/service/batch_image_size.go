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
