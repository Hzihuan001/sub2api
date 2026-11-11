package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type imagePartialRead struct{ read bool }

func (r *imagePartialRead) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.ErrUnexpectedEOF
	}
	r.read = true
	return copy(p, "partial-corrupt-data"), nil
}
func (r *imagePartialRead) Close() error { return nil }

func TestImageResultDownloadRetriesOnlyTheSavedObject(t *testing.T) {
	for _, first := range []string{"body_reset", "short_length", "server_error", "http2"} {
		t.Run(first, func(t *testing.T) {
			calls := 0
			u := NewImageResultUploader(nil, "", 0, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "https://cdn.example/image.png", req.URL.String())
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(pngBytes)), ContentLength: int64(len(pngBytes))}
				if calls == 1 {
					switch first {
					case "body_reset":
						resp.Body = &imagePartialRead{}
					case "short_length":
						resp.Body = io.NopCloser(strings.NewReader("partial"))
					case "server_error":
						resp.StatusCode = 503
					case "http2":
						return nil, errors.New("http2: client connection force closed via ClientConn.Close")
					}
				}
				return resp, nil
			})})
			data, contentType, err := u.download(context.Background(), "https://cdn.example/image.png")
			require.NoError(t, err)
			require.Equal(t, 2, calls)
			require.Equal(t, pngBytes, data)
			require.Equal(t, "image/png", contentType)
		})
	}
}

func TestImageResultDownloadDoesNotRetryRejectedOrOversizedObjects(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			u := NewImageResultUploader(nil, "", 3, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(pngBytes))}, nil
			})})
			_, _, err := u.download(context.Background(), "https://cdn.example/image.png")
			require.Error(t, err)
			require.Equal(t, 1, calls)
		})
	}
}

func TestImageResultDownloadCancellationStopsReconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	u := NewImageResultUploader(nil, "", 0, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("busy"))}, nil
	})})
	_, _, err := u.download(ctx, "https://cdn.example/image.png")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}

func TestImageResultDownloadRejectsUnsolicitedPartialContent(t *testing.T) {
	for _, status := range []int{http.StatusPartialContent, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			u := NewImageResultUploader(nil, "", 0, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"image/png"}, "Content-Range": {"bytes 0-7/1000"}}, Body: io.NopCloser(bytes.NewReader(pngBytes)), ContentLength: int64(len(pngBytes))}, nil
			})})
			data, _, err := u.download(context.Background(), "https://cdn.example/image.png")
			require.ErrorContains(t, err, "partial content")
			require.Empty(t, data)
			require.Equal(t, 1, calls)
		})
	}
}
