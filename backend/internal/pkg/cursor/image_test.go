package cursor

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func pngDataURI(t *testing.T, width, height int) (uri string, raw []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	raw = buf.Bytes()
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw), raw
}

func TestParseImageDataURIReadsDimensions(t *testing.T) {
	t.Parallel()
	uri, raw := pngDataURI(t, 7, 5)

	img, err := ParseImageDataURI(uri)
	require.NoError(t, err)
	require.Equal(t, raw, img.Data)
	require.Equal(t, int32(7), img.Width)
	require.Equal(t, int32(5), img.Height)
}

func TestParseImageDataURIToleratesWhitespaceAndCase(t *testing.T) {
	t.Parallel()
	_, raw := pngDataURI(t, 4, 4)
	// Some clients wrap long payloads and upper-case the scheme.
	wrapped := "DATA:image/PNG;BASE64," + insertEvery(base64.StdEncoding.EncodeToString(raw), 40, "\n")

	img, err := ParseImageDataURI(wrapped)
	require.NoError(t, err)
	require.Equal(t, raw, img.Data)
}

func TestParseImageDataURIUnknownFormatStillEncodes(t *testing.T) {
	t.Parallel()
	// WebP has no standard-library decoder: the bytes still travel, just
	// without a Dimension.
	payload := []byte("RIFF____WEBPVP8 not-a-real-webp")
	uri := "data:image/webp;base64," + base64.StdEncoding.EncodeToString(payload)

	img, err := ParseImageDataURI(uri)
	require.NoError(t, err)
	require.Equal(t, payload, img.Data)
	require.Zero(t, img.Width)
	require.Zero(t, img.Height)
}

func TestParseImageDataURIRejectsNonDataURIs(t *testing.T) {
	t.Parallel()
	for _, uri := range []string{
		"https://example.com/cat.png",
		"",
		"data:image/png,not-base64-encoded",
		"data:image/png;base64",
		"data:image/png;base64,",
	} {
		_, err := ParseImageDataURI(uri)
		require.ErrorIs(t, err, ErrNotImageDataURI, "uri %q", uri)
	}
}

func TestParseImageDataURIRejectsNonImageMediaTypes(t *testing.T) {
	t.Parallel()
	_, err := ParseImageDataURI("data:application/pdf;base64," + base64.StdEncoding.EncodeToString([]byte("%PDF")))
	require.ErrorIs(t, err, ErrNotImageDataURI)
}

func TestParseImageDataURIRejectsUndecodablePayload(t *testing.T) {
	t.Parallel()
	_, err := ParseImageDataURI("data:image/png;base64,!!!not-base64!!!")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotImageDataURI)
}

func insertEvery(s string, n int, sep string) string {
	var out bytes.Buffer
	for i, r := range s {
		if i > 0 && i%n == 0 {
			out.WriteString(sep)
		}
		out.WriteRune(r)
	}
	return out.String()
}
