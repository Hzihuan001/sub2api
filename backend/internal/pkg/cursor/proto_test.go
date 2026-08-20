package cursor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriterDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	var w Writer
	w.WriteString(1, "hello")
	w.WriteBool(2, true)
	w.WriteInt32(3, 42)
	w.WriteInt64(4, 1<<40)
	w.WriteBytes(5, []byte{0xde, 0xad, 0xbe, 0xef})
	var sub Writer
	sub.WriteString(1, "nested")
	w.WriteMessage(6, sub.Bytes())

	f, err := Decode(w.Bytes())
	require.NoError(t, err)
	require.Equal(t, "hello", f.String(1))
	require.True(t, f.Bool(2))
	require.Equal(t, int32(42), f.Int32(3))
	require.Equal(t, int64(1<<40), f.Int64(4))
	require.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, f.Bytes(5))

	nf, err := Decode(f.Bytes(6))
	require.NoError(t, err)
	require.Equal(t, "nested", nf.String(1))
}

func TestDecodeRepeatedField(t *testing.T) {
	t.Parallel()
	var w Writer
	w.WriteString(1, "a")
	w.WriteString(1, "b")
	w.WriteString(1, "c")

	f, err := Decode(w.Bytes())
	require.NoError(t, err)
	require.Equal(t, [][]byte{[]byte("a"), []byte("b"), []byte("c")}, f.AllBytes(1))
	// A single accessor returns the last occurrence.
	require.Equal(t, "c", f.String(1))
	require.True(t, f.Has(1))
	require.False(t, f.Has(2))
}

func TestReadVarint(t *testing.T) {
	t.Parallel()
	var w Writer
	w.WriteVarint(300)
	v, n, err := ReadVarint(w.Bytes())
	require.NoError(t, err)
	require.Equal(t, uint64(300), v)
	require.Equal(t, 2, n)
}

func TestReadTag(t *testing.T) {
	t.Parallel()
	var w Writer
	w.WriteTag(5, wireLen)
	field, wt, n, err := ReadTag(w.Bytes())
	require.NoError(t, err)
	require.Equal(t, 5, field)
	require.Equal(t, wireLen, wt)
	require.Equal(t, 1, n)
}

func TestReadVarintTruncated(t *testing.T) {
	t.Parallel()
	// A dangling continuation byte with no terminator is truncated.
	_, _, err := ReadVarint([]byte{0x80})
	require.Error(t, err)
}

func TestDecodeTruncatedLengthDelimited(t *testing.T) {
	t.Parallel()
	// tag for field 1 (LEN) + declared length 5 but only 2 bytes follow.
	_, err := Decode([]byte{0x0a, 0x05, 0x01, 0x02})
	require.Error(t, err)
}
