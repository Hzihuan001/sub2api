// Package cursor is a self-contained upstream protocol client for Cursor's
// subscription API (api2.cursor.sh). It implements just enough of the Connect
// RPC wire format (protobuf + 5-byte envelope framing) to talk to two
// endpoints: AiService/AvailableModels (unary) and
// ChatService/StreamUnifiedChatWithTools (server streaming).
//
// The package is intentionally dependency-light: only the standard library and
// github.com/google/uuid (already a module dependency) are used. Protobuf
// messages are hand-encoded/decoded because they are small and fixed; pulling
// in protoc/google.golang.org/protobuf would be overkill.
//
// Provenance / license: the reference implementation daotor/go-cursor (which
// the task suggested adapting) is no longer reachable on GitHub (404 at time of
// writing), so no code was copied from it. The wire format here is Cursor's own
// Connect protocol; protobuf field numbers were cross-checked against the
// public schema in kaitranntt/ccs (src/cursor/cursor-protobuf-schema.ts, aligned
// to Cursor 2.6.22), the named aiserver.v1 definitions extracted in
// eisbaw/cursor_api_demo (cursor-grpc/server_full.proto), and wisdgod/cursor-api.
// Field numbers and the Connect framing are protocol facts, not copyrightable
// expression, and every line below is original Go.
package cursor

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Protobuf wire types (https://protobuf.dev/programming-guides/encoding/).
const (
	wireVarint  = 0
	wireFixed64 = 1
	wireLen     = 2
	wireFixed32 = 5
)

var errVarintOverflow = errors.New("cursor: varint overflows 64 bits")

// Writer builds a protobuf message by appending length-delimited and varint
// fields. It is a thin, allocation-friendly helper around a byte slice; the
// zero value is ready to use.
type Writer struct {
	buf []byte
}

// Bytes returns the accumulated wire bytes. The slice aliases the writer's
// internal buffer; copy it if you need to retain it across further writes.
func (w *Writer) Bytes() []byte { return w.buf }

// Reset truncates the buffer while keeping its capacity for reuse.
func (w *Writer) Reset() { w.buf = w.buf[:0] }

// WriteVarint appends a base-128 varint (little-endian groups, high bit as
// continuation) — the primitive behind every non-fixed protobuf value.
func (w *Writer) WriteVarint(v uint64) {
	for v >= 0x80 {
		w.buf = append(w.buf, byte(v)|0x80)
		v >>= 7
	}
	w.buf = append(w.buf, byte(v))
}

// WriteTag appends a field key: (field_number << 3) | wire_type.
func (w *Writer) WriteTag(field, wireType int) {
	w.WriteVarint(uint64(field)<<3 | uint64(wireType))
}

// WriteBytes writes a length-delimited (wire type 2) field.
func (w *Writer) WriteBytes(field int, b []byte) {
	w.WriteTag(field, wireLen)
	w.WriteVarint(uint64(len(b)))
	w.buf = append(w.buf, b...)
}

// WriteString writes a length-delimited (wire type 2) UTF-8 string field.
func (w *Writer) WriteString(field int, s string) {
	w.WriteTag(field, wireLen)
	w.WriteVarint(uint64(len(s)))
	w.buf = append(w.buf, s...)
}

// WriteMessage writes a nested message (already-encoded bytes) as a
// length-delimited field. Semantically identical to WriteBytes; named
// separately for readability at call sites.
func (w *Writer) WriteMessage(field int, sub []byte) {
	w.WriteBytes(field, sub)
}

// WriteBool writes a varint field carrying 0 or 1.
func (w *Writer) WriteBool(field int, v bool) {
	w.WriteTag(field, wireVarint)
	if v {
		w.WriteVarint(1)
		return
	}
	w.WriteVarint(0)
}

// WriteInt32 writes a protobuf int32 (varint, sign-extended to 64 bits for
// negative values, matching the reference protobuf encoding).
func (w *Writer) WriteInt32(field int, v int32) {
	w.WriteTag(field, wireVarint)
	w.WriteVarint(uint64(int64(v)))
}

// WriteInt64 writes a protobuf int64 (varint).
func (w *Writer) WriteInt64(field int, v int64) {
	w.WriteTag(field, wireVarint)
	w.WriteVarint(uint64(v))
}

// ReadVarint decodes a base-128 varint from the front of data, returning the
// value and the number of bytes consumed.
func ReadVarint(data []byte) (value uint64, n int, err error) {
	var s uint
	for i := 0; i < len(data); i++ {
		b := data[i]
		if i == 9 && b > 1 {
			return 0, 0, errVarintOverflow
		}
		if b < 0x80 {
			return value | uint64(b)<<s, i + 1, nil
		}
		value |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, 0, io.ErrUnexpectedEOF
}

// ReadTag decodes a field key, returning the field number, wire type, and bytes
// consumed.
func ReadTag(data []byte) (field int, wireType int, n int, err error) {
	v, m, err := ReadVarint(data)
	if err != nil {
		return 0, 0, 0, err
	}
	return int(v >> 3), int(v & 7), m, nil
}

// Value is one decoded field occurrence. For varint/fixed32/fixed64 the numeric
// value lives in Varint; for length-delimited fields the raw bytes live in
// Bytes (aliasing the source buffer).
type Value struct {
	WireType int
	Varint   uint64
	Bytes    []byte
}

// Fields is a decoded protobuf message: field number -> occurrences in wire
// order. A map-of-slices (rather than the map[int][]byte hinted at in the spec)
// is used so that repeated fields — e.g. repeated Model models=2 — are fully
// preserved instead of collapsing to last-wins.
type Fields map[int][]Value

// Decode parses a protobuf message body into Fields. Unknown fields are kept
// (they are decoded generically by wire type); group wire types (3/4) are
// rejected as they never appear in Cursor messages.
func Decode(data []byte) (Fields, error) {
	fields := make(Fields)
	pos := 0
	for pos < len(data) {
		field, wt, n, err := ReadTag(data[pos:])
		if err != nil {
			return nil, err
		}
		pos += n
		switch wt {
		case wireVarint:
			v, m, err := ReadVarint(data[pos:])
			if err != nil {
				return nil, err
			}
			pos += m
			fields[field] = append(fields[field], Value{WireType: wt, Varint: v})
		case wireLen:
			l, m, err := ReadVarint(data[pos:])
			if err != nil {
				return nil, err
			}
			pos += m
			if l > uint64(len(data)-pos) {
				return nil, io.ErrUnexpectedEOF
			}
			b := data[pos : pos+int(l)]
			pos += int(l)
			fields[field] = append(fields[field], Value{WireType: wt, Bytes: b})
		case wireFixed64:
			if pos+8 > len(data) {
				return nil, io.ErrUnexpectedEOF
			}
			fields[field] = append(fields[field], Value{WireType: wt, Varint: binary.LittleEndian.Uint64(data[pos:])})
			pos += 8
		case wireFixed32:
			if pos+4 > len(data) {
				return nil, io.ErrUnexpectedEOF
			}
			fields[field] = append(fields[field], Value{WireType: wt, Varint: uint64(binary.LittleEndian.Uint32(data[pos:]))})
			pos += 4
		default:
			return nil, fmt.Errorf("cursor: unsupported wire type %d for field %d", wt, field)
		}
	}
	return fields, nil
}

// Has reports whether the field appeared at least once.
func (f Fields) Has(field int) bool { return len(f[field]) > 0 }

// Varint returns the last varint/fixed value for the field (0 if absent).
func (f Fields) Varint(field int) uint64 {
	vs := f[field]
	if len(vs) == 0 {
		return 0
	}
	return vs[len(vs)-1].Varint
}

// Bool returns the last varint value interpreted as a bool.
func (f Fields) Bool(field int) bool { return f.Varint(field) != 0 }

// Int32 returns the last varint value as an int32.
func (f Fields) Int32(field int) int32 { return int32(f.Varint(field)) }

// Int64 returns the last varint value as an int64.
func (f Fields) Int64(field int) int64 { return int64(f.Varint(field)) }

// Bytes returns the last length-delimited value for the field (nil if absent).
func (f Fields) Bytes(field int) []byte {
	vs := f[field]
	for i := len(vs) - 1; i >= 0; i-- {
		if vs[i].WireType == wireLen {
			return vs[i].Bytes
		}
	}
	return nil
}

// String returns the last length-delimited value decoded as a string.
func (f Fields) String(field int) string { return string(f.Bytes(field)) }

// AllBytes returns every length-delimited value for the field, in wire order —
// the accessor for repeated message/string fields.
func (f Fields) AllBytes(field int) [][]byte {
	vs := f[field]
	if len(vs) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(vs))
	for _, v := range vs {
		if v.WireType == wireLen {
			out = append(out, v.Bytes)
		}
	}
	return out
}
