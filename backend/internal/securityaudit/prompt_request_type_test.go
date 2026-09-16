package securityaudit

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPromptRequestTypeSurvivesAllSnapshotModes(t *testing.T) {
	for _, kind := range []string{"sync", "stream", "ws_v2", "live", "cyber", ""} {
		t.Run(kind, func(t *testing.T) {
			req := Request{Protocol: "openai_responses", RequestType: kind,
				Body: []byte(`{"input":"hello"}`)}
			for _, extract := range []func(Request) (PromptSnapshot, error){
				ExtractPromptSnapshot, ExtractLatestUserPromptSnapshot,
				func(r Request) (PromptSnapshot, error) { return ExtractBlockingPromptSnapshot(r, true) },
			} {
				snapshot, err := extract(req.Clone())
				require.NoError(t, err)
				require.Equal(t, kind, snapshot.RequestType)
			}
		})
	}
}

func TestPromptRequestTypeQueryUsesBoundParameter(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/events?request_type=stream", nil)
	filter, err := eventFilterFromQuery(c)
	require.NoError(t, err)
	require.Equal(t, "stream", filter.RequestType)
	where, args := buildEventWhere(filter, 3)
	require.Contains(t, where, "COALESCE(e.request_type, '')=$3")
	require.Equal(t, []any{"stream"}, args)

	// Legacy records have no known request type; an unfiltered query includes them.
	where, args = buildEventWhere(EventFilter{}, 1)
	require.NotContains(t, where, "request_type")
	require.Empty(t, args)
	injected := "stream' OR TRUE --"
	where, args = buildEventWhere(EventFilter{RequestType: injected}, 1)
	require.NotContains(t, where, injected)
	require.Equal(t, []any{injected}, args)
}
