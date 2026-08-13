package cursor

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"strings"

	// Registered for image.DecodeConfig so ParseImageDataURI can read pixel
	// dimensions from a header without decoding the whole bitmap.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// MaxImageBytes caps the decoded size of a single attached image. Cursor's own
// client downscales before upload; this is a memory guard, not a protocol
// limit.
const MaxImageBytes = 16 << 20 // 16 MiB

// ErrNotImageDataURI reports that a string is not a base64 image data URI and
// therefore cannot be attached natively (callers should degrade to text).
var ErrNotImageDataURI = errors.New("cursor: not a base64 image data URI")

// ParseImageDataURI converts an OpenAI `image_url` data URI
// (data:image/png;base64,...) into an Image ready for encodeImage. Remote http
// (s) URLs are rejected with ErrNotImageDataURI: fetching them server-side
// would turn the gateway into an SSRF relay, so callers degrade those to text.
//
// Width/Height are filled in for the formats the standard library can read a
// header for (PNG/JPEG/GIF); other formats (e.g. WebP) still encode fine, just
// without a Dimension.
func ParseImageDataURI(uri string) (Image, error) {
	raw := strings.TrimSpace(uri)
	if !strings.HasPrefix(strings.ToLower(raw), "data:") {
		return Image{}, ErrNotImageDataURI
	}
	comma := strings.IndexByte(raw, ',')
	if comma < 0 {
		return Image{}, ErrNotImageDataURI
	}
	header := strings.ToLower(raw[len("data:"):comma])
	payload := raw[comma+1:]

	if !strings.Contains(header, ";base64") {
		return Image{}, ErrNotImageDataURI
	}
	mediaType, _, _ := strings.Cut(header, ";")
	if mediaType != "" && !strings.HasPrefix(mediaType, "image/") {
		return Image{}, fmt.Errorf("cursor: unsupported data URI media type %q: %w", mediaType, ErrNotImageDataURI)
	}

	data, err := decodeBase64(payload)
	if err != nil {
		return Image{}, fmt.Errorf("cursor: decode image data URI: %w", err)
	}
	if len(data) == 0 {
		return Image{}, ErrNotImageDataURI
	}
	if len(data) > MaxImageBytes {
		return Image{}, fmt.Errorf("cursor: image is %d bytes, over the %d byte limit", len(data), MaxImageBytes)
	}

	img := Image{Data: data}
	if cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(data)); cfgErr == nil {
		img.Width = int32(cfg.Width)
		img.Height = int32(cfg.Height)
	}
	return img, nil
}

// decodeBase64 accepts padded and unpadded base64, tolerating the embedded
// newlines some clients emit when wrapping long payloads.
func decodeBase64(payload string) ([]byte, error) {
	cleaned := strings.NewReplacer("\n", "", "\r", "", " ", "", "\t", "").Replace(payload)
	if data, err := base64.StdEncoding.DecodeString(cleaned); err == nil {
		return data, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(cleaned, "="))
}
