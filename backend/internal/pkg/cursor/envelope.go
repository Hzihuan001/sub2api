package cursor

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
)

// Connect envelope framing.
//
// Streaming RPCs (content-type application/connect+proto) frame every message
// as: [flag:1 byte][payload_len:4 bytes big-endian uint32][payload]. The flag
// byte is a bitfield, interpreted per the Connect spec:
//
//	bit 0x01 -> payload is gzip-compressed
//	bit 0x02 -> this is the end-of-stream (trailer) frame; payload is JSON
//	            ({} for a clean end, or {"error": ...} for a failure)
//
// So the four flags called out in the task map onto the two bits:
//
//	0x00 data frame, uncompressed protobuf
//	0x01 data frame, gzip(protobuf)
//	0x02 end-of-stream frame, uncompressed JSON
//	0x03 end-of-stream frame, gzip(JSON)
//
// Treating flag as a bitfield (rather than four opaque enum values) is the
// correct reading: e.g. a 0x02 trailer is NOT gzipped, so blindly gunzipping
// every non-zero flag — as some reverse-engineered clients do and merely
// swallow the error — is wrong. Here compression is decided by (flag & 0x01)
// alone.
//
// Unary RPCs (content-type application/proto, e.g. AiService/AvailableModels)
// are NOT enveloped: the HTTP request/response body is the raw protobuf message
// with no 5-byte prefix. This matches daotor/go-cursor's documented split of
// SetUnaryHeaders (application/proto) vs SetStreamHeaders
// (application/connect+proto). Callers therefore use EncodeFrame / FrameReader
// only for the streaming chat endpoint, and send/parse raw proto for unary.
const (
	flagCompressed byte = 0x01
	flagEndStream  byte = 0x02
)

// maxFrameSize caps a single frame's declared payload length so a malformed or
// hostile length prefix cannot trigger an unbounded allocation.
const maxFrameSize = 64 << 20 // 64 MiB

// maxDecompressedFrameSize caps a frame's payload *after* gunzip. Bounding the
// compressed length alone is not enough: gzip reaches ~1000:1 on repetitive
// input, so a frame well under maxFrameSize can still expand into gigabytes.
const maxDecompressedFrameSize = 64 << 20 // 64 MiB

// EncodeFrame wraps a protobuf payload in a single Connect data frame. When
// compress is true the payload is gzipped and the compressed bit is set. It
// only ever produces data frames (flag 0x00/0x01); end-of-stream frames are
// emitted by the server, never the client.
func EncodeFrame(payload []byte, compress bool) []byte {
	flag := byte(0)
	body := payload
	if compress {
		body = gzipBytes(payload)
		flag |= flagCompressed
	}
	return encodeRawFrame(flag, body)
}

// encodeRawFrame writes an arbitrary flag + already-final payload. Kept
// unexported so tests can synthesize trailer/compressed frames while the public
// surface stays limited to well-formed data frames.
func encodeRawFrame(flag byte, body []byte) []byte {
	out := make([]byte, 5+len(body))
	out[0] = flag
	binary.BigEndian.PutUint32(out[1:5], uint32(len(body)))
	copy(out[5:], body)
	return out
}

// Frame is one decoded Connect frame with its payload already decompressed.
type Frame struct {
	// Flag is the raw flag byte as received.
	Flag byte
	// Compressed reports whether the frame arrived gzip-compressed (flag&0x01).
	Compressed bool
	// EndStream reports whether this is the trailer frame (flag&0x02); its
	// Payload is JSON rather than protobuf.
	EndStream bool
	// Payload is the (decompressed) frame body.
	Payload []byte
}

// FrameReader reads a stream of Connect frames from an io.Reader. It handles
// sticky packets (many frames in one read) and half packets (a frame split
// across reads) because it uses io.ReadFull for both the 5-byte header and the
// payload, which blocks until exactly the needed bytes arrive or the stream
// ends. gzip frames are transparently decompressed.
type FrameReader struct {
	r      *bufio.Reader
	header [5]byte
}

// NewFrameReader wraps r. The reader is buffered internally.
func NewFrameReader(r io.Reader) *FrameReader {
	return &FrameReader{r: bufio.NewReader(r)}
}

// Next returns the next frame. It returns io.EOF cleanly when the stream ends
// exactly on a frame boundary, and io.ErrUnexpectedEOF when it ends mid-frame.
func (fr *FrameReader) Next() (*Frame, error) {
	if _, err := io.ReadFull(fr.r, fr.header[:]); err != nil {
		// io.EOF here means a clean end at a frame boundary; pass it through.
		return nil, err
	}
	flag := fr.header[0]
	length := binary.BigEndian.Uint32(fr.header[1:5])
	if length > maxFrameSize {
		return nil, fmt.Errorf("cursor: frame length %d exceeds max %d", length, maxFrameSize)
	}

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(fr.r, payload); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}

	compressed := flag&flagCompressed != 0
	if compressed && length > 0 {
		dec, err := gunzip(payload)
		if err != nil {
			return nil, fmt.Errorf("cursor: decompress frame: %w", err)
		}
		payload = dec
	}

	return &Frame{
		Flag:       flag,
		Compressed: compressed,
		EndStream:  flag&flagEndStream != 0,
		Payload:    payload,
	}, nil
}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write(b)
	_ = gw.Close()
	return buf.Bytes()
}

func gunzip(b []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	// One byte past the cap, so a payload that lands exactly on it still decodes
	// while anything larger is rejected instead of silently truncated.
	out, err := io.ReadAll(io.LimitReader(gr, maxDecompressedFrameSize+1))
	if err != nil {
		return nil, err
	}
	if len(out) > maxDecompressedFrameSize {
		return nil, fmt.Errorf("cursor: decompressed frame exceeds max %d bytes", maxDecompressedFrameSize)
	}
	return out, nil
}
