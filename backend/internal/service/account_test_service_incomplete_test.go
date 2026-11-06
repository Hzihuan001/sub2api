package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestScheduledOpenAIIncompletePreservesFailureReasonWithoutTransportRetry(t *testing.T) {
	cases := []struct {
		name  string
		event string
		want  string
	}{
		{
			name:  "output limit",
			event: `{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`,
			want:  "OpenAI response incomplete: max_output_tokens",
		},
		{
			name:  "missing reason",
			event: `{"type":"response.incomplete","response":{"status":"incomplete"}}`,
			want:  "OpenAI response incomplete",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
			err := (&AccountTestService{}).processOpenAIStream(c, strings.NewReader("data: "+tt.event+"\n\n"))
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			responseText, errorMsg := parseTestSSEOutput(recorder.Body.String())
			if responseText != "" || errorMsg != tt.want {
				t.Fatalf("saved result text=%q error=%q, want %q", responseText, errorMsg, tt.want)
			}
			if isRetryablePelicanTransportFailure(&ScheduledTestResult{Status: "failed", ErrorMessage: errorMsg}, nil) {
				t.Fatal("explicit incomplete result must not be retried as a transport disconnect")
			}
		})
	}
}
