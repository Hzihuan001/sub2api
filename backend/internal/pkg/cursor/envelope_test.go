package cursor

import (
	"bytes"
	"io"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
)

func TestEncodeFrameRoundTrip(t *testing.T) {
	t.Parallel()
	payload := []byte("hello proto")
	frame := EncodeFrame(payload, false)
	require.Equal(t, byte(0x00), frame[0])

	fr := NewFrameReader(bytes.NewReader(frame))
	f, err := fr.Next()
	require.NoError(t, err)
	require.False(t, f.Compressed)
	require.False(t, f.EndStream)
	require.Equal(t, payload, f.Payload)

	_, err = fr.Next()
	require.ErrorIs(t, err, io.EOF)
}

func TestEncodeFrameGzipRoundTrip(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte("compress me "), 200)
	frame := EncodeFrame(payload, true)
	require.Equal(t, flagCompressed, frame[0])
	// Compressed frame should be smaller than raw payload + header.
	require.Less(t, len(frame), len(payload))

	fr := NewFrameReader(bytes.NewReader(frame))
	f, err := fr.Next()
	require.NoError(t, err)
	require.True(t, f.Compressed)
	require.Equal(t, payload, f.Payload)
}

// A frame whose compressed size passes maxFrameSize can still expand past what
// the gateway is willing to hold. Bounding only the wire length left a
// decompression bomb: ~64 KiB of zeros gunzips to 64 MiB.
func TestFrameReaderRejectsDecompressionBomb(t *testing.T) {
	t.Parallel()
	bomb := gzipBytes(make([]byte, maxDecompressedFrameSize+1))
	require.Less(t, len(bomb), maxFrameSize, "the bomb must pass the wire-length cap to test the decompressed one")

	fr := NewFrameReader(bytes.NewReader(encodeRawFrame(flagCompressed, bomb)))
	_, err := fr.Next()
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max")
}

// The cap is a ceiling, not a budget: a payload landing exactly on it decodes.
func TestFrameReaderAcceptsPayloadAtDecompressedCap(t *testing.T) {
	t.Parallel()
	payload := make([]byte, maxDecompressedFrameSize)
	fr := NewFrameReader(bytes.NewReader(encodeRawFrame(flagCompressed, gzipBytes(payload))))
	f, err := fr.Next()
	require.NoError(t, err)
	require.Len(t, f.Payload, maxDecompressedFrameSize)
}

func TestFrameReaderStickyPackets(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	buf.Write(EncodeFrame([]byte("one"), false))
	buf.Write(EncodeFrame([]byte("two"), true))
	buf.Write(encodeRawFrame(flagEndStream, []byte("{}")))

	fr := NewFrameReader(&buf)

	f1, err := fr.Next()
	require.NoError(t, err)
	require.Equal(t, []byte("one"), f1.Payload)

	f2, err := fr.Next()
	require.NoError(t, err)
	require.True(t, f2.Compressed)
	require.Equal(t, []byte("two"), f2.Payload)

	f3, err := fr.Next()
	require.NoError(t, err)
	require.True(t, f3.EndStream)
	require.False(t, f3.Compressed)
	require.Equal(t, []byte("{}"), f3.Payload)

	_, err = fr.Next()
	require.ErrorIs(t, err, io.EOF)
}

func TestFrameReaderHalfPackets(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	buf.Write(EncodeFrame([]byte("alpha"), false))
	buf.Write(EncodeFrame(bytes.Repeat([]byte("x"), 50), true))

	// OneByteReader forces a partial read on every call, exercising the
	// half-packet handling of io.ReadFull inside FrameReader.
	fr := NewFrameReader(iotest.OneByteReader(bytes.NewReader(buf.Bytes())))

	f1, err := fr.Next()
	require.NoError(t, err)
	require.Equal(t, []byte("alpha"), f1.Payload)

	f2, err := fr.Next()
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte("x"), 50), f2.Payload)
}

func TestFrameReaderUnexpectedEOF(t *testing.T) {
	t.Parallel()
	frame := EncodeFrame([]byte("truncated payload"), false)
	// Drop the last few payload bytes so the frame ends mid-body.
	fr := NewFrameReader(bytes.NewReader(frame[:len(frame)-4]))
	_, err := fr.Next()
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}
