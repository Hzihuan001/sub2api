//go:build unit

package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type keyRouteUsagePrefixNativeUpstream struct {
	service.HTTPUpstream
	accountIDs []int64
}

func (u *keyRouteUsagePrefixNativeUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	// A real Responses parser buffers this prefix, then returns nil plus a
	// retryable overload. Its input usage must still prohibit another account.
	const payload = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"synthetic_usage_prefix\",\"usage\":{\"input_tokens\":7,\"output_tokens\":0}}}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"synthetic_usage_prefix\",\"status\":\"failed\",\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded. Please try again later.\"}}}\n\n"
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
}

func TestKeyRouteFirstContentNativeNilResultUsageNeverRetriesAccounts(t *testing.T) {
	t.Run("native nil-result retryable boundary", func(t *testing.T) {
		upstream := &keyRouteUsagePrefixNativeUpstream{}
		h := newOpenAIResponsesFailoverTestHandler(t, upstream)
		h.cfg.Gateway.MaxLineSize = 1 << 20
		guard := keyrouting.NewFirstContentGuard(0)
		defer guard.Finish()
		ctx := keyrouting.WithFirstContentGuard(context.Background(), guard)
		c, rec := newOpenAIResponsesFailoverTestContext(t, ctx)
		body := []byte(`{"model":"gpt-5.1","stream":true,"input":"hello"}`)
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		result, err := h.gatewayService.Forward(ctx, c, &service.Account{
			ID: 1, Name: "synthetic-native-prefix", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Credentials: map[string]any{"access_token": "synthetic"},
		}, body)
		require.Nil(t, result)
		var retry *service.UpstreamFailoverError
		require.True(t, errors.As(err, &retry), "fixture must hit the native retry branch, not an ordinary parser error")
		require.True(t, guard.ReplayBlocked())
		require.False(t, c.Writer.Written())
		require.Zero(t, rec.Body.Len())
	})
	t.Run("real handler returns error without second account", func(t *testing.T) {
		upstream := &keyRouteUsagePrefixNativeUpstream{}
		h := newOpenAIResponsesFailoverTestHandler(t, upstream)
		h.cfg.Gateway.MaxLineSize = 1 << 20
		guard := keyrouting.NewFirstContentGuard(0)
		defer guard.Finish()
		ctx := keyrouting.WithFirstContentGuard(context.Background(), guard)
		c, rec := newOpenAIResponsesFailoverTestContext(t, ctx)
		body := []byte(`{"model":"gpt-5.1","stream":true,"input":"hello"}`)
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		writer := newFirstContentWriter(c.Writer, guard, true)
		c.Writer = writer
		h.Responses(c)
		writer.Finalize(false)
		require.Equal(t, []int64{1}, upstream.accountIDs, "known usage must stop both same-account retries and next-account dispatch")
		require.True(t, guard.ReplayBlocked())
		require.NotEmpty(t, rec.Body.String(), "the native non-retry terminal path must not return an empty success")
		require.Contains(t, rec.Body.String(), "upstream_error")
		require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest)
	})
}
