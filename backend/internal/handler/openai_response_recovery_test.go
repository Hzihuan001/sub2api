package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsOpenAIResponseReferenceRejectedErrorIsNarrow(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"previous_response_not_found","message":"not found"}}`,
		`{"error":{"param":"previous_response_id","message":"previous_response_id is not available for this user"}}`,
		`{"error":{"message":"No response found with id resp_123"}}`,
	} {
		err := &service.UpstreamFailoverError{StatusCode: http.StatusBadRequest, ResponseBody: []byte(body)}
		require.True(t, isOpenAIResponseReferenceRejectedError(err), body)
	}
	for _, err := range []*service.UpstreamFailoverError{
		{StatusCode: http.StatusBadRequest, ResponseBody: []byte(`{"error":{"message":"invalid model"}}`)},
		{StatusCode: http.StatusBadRequest, ResponseBody: []byte(`{"error":{"param":"model","message":"previous_response_id is not available"}}`)},
		{StatusCode: http.StatusNotFound, ResponseBody: []byte(`{"error":{"message":"resource not found"}}`)},
		{StatusCode: http.StatusServiceUnavailable, ResponseBody: []byte(`{"error":{"code":"previous_response_not_found"}}`)},
		{StatusCode: http.StatusBadRequest},
	} {
		require.False(t, isOpenAIResponseReferenceRejectedError(err), err)
	}
}

func TestDropOpenAIStoredResponseReferencesPreservesCurrentRequest(t *testing.T) {
	original := []byte(`{"model":"gpt-5.6-sol","previous_response_id":"resp_from_a","conversation":{"id":"conv_a"},"instructions":"keep this","input":[{"type":"item_reference","id":"item_from_a"},{"type":"function_call_output","call_id":"call_from_a","output":"tool result in current request"},{"role":"user","content":"continue this exact request"},{"role":"user","content":[{"type":"input_image","file_id":"file_current"}]}],"tools":[{"type":"function","name":"lookup"}]}`)
	got, err := dropOpenAIStoredResponseReferences(original)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-5.6-sol","instructions":"keep this","input":[{"type":"function_call_output","call_id":"call_from_a","output":"tool result in current request"},{"role":"user","content":"continue this exact request"},{"role":"user","content":[{"type":"input_image","file_id":"file_current"}]}],"tools":[{"type":"function","name":"lookup"}]}`, string(got))
}

func TestDropOpenAIStoredResponseForFallbackClearsPinAndKeepsRequestContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"gpt-5.6-sol","previous_response_id":"resp_from_a","conversation":{"id":"conv_a"},"instructions":"retain","input":[{"type":"item_reference","id":"item_a"},{"role":"user","content":"continue this"}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	ctx := context.WithValue(request.Context(), ctxkey.KeyRoutingResponseSourceGroupID, int64(2))
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseSourceAccountID, int64(42))
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseOwnerGroupID, int64(2))
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingActive, true)
	request = request.WithContext(ctx)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = request

	updated, err := dropOpenAIStoredResponseForFallback(c, []byte(body), 2)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-5.6-sol","instructions":"retain","input":[{"role":"user","content":"continue this"}]}`, string(updated))
	require.EqualValues(t, 0, c.Request.Context().Value(ctxkey.KeyRoutingResponseSourceGroupID))
	require.EqualValues(t, 0, c.Request.Context().Value(ctxkey.KeyRoutingResponseSourceAccountID))
	require.EqualValues(t, 0, c.Request.Context().Value(ctxkey.KeyRoutingResponseOwnerGroupID))
	require.Equal(t, true, c.Request.Context().Value(ctxkey.KeyRoutingActive))
	require.EqualValues(t, 2, c.Request.Context().Value(ctxkey.KeyRoutingPrimaryGroupID))
}

func TestEnableOpenAIResponseAccountFallbackPreservesBodyAndReleasesSourcePin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"m","previous_response_id":"resp_source"}`))
	ctx := context.WithValue(request.Context(), ctxkey.KeyRoutingResponseSourceGroupID, int64(2))
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseSourceAccountID, int64(42))
	request = request.WithContext(ctx)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = request

	require.True(t, enableOpenAIResponseAccountFallback(c, 2))
	require.EqualValues(t, 0, c.Request.Context().Value(ctxkey.KeyRoutingResponseSourceGroupID))
	require.EqualValues(t, 0, c.Request.Context().Value(ctxkey.KeyRoutingResponseSourceAccountID))
	require.Equal(t, true, c.Request.Context().Value(ctxkey.KeyRoutingActive))
	require.EqualValues(t, 2, c.Request.Context().Value(ctxkey.KeyRoutingPrimaryGroupID))
	value, ok := c.Get("custom.key_route_base_context")
	require.True(t, ok)
	require.Same(t, c.Request.Context(), value)
	body, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"m","previous_response_id":"resp_source"}`, string(body))
}
