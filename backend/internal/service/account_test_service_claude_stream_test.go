package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestProcessClaudeStreamRequiresTerminalEvent(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		wantError string
	}{
		{
			name:      "empty stream is incomplete",
			wantError: "Anthropic stream ended before message_stop",
		},
		{
			name:      "partial content is incomplete",
			stream:    "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n",
			wantError: "Anthropic stream ended before message_stop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
			err := (&AccountTestService{}).processClaudeStream(ctx, strings.NewReader(tt.stream))
			if err == nil || err.Error() != tt.wantError {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
			responseText, errorMsg := parseTestSSEOutput(recorder.Body.String())
			if responseText == "" && tt.stream != "" {
				t.Fatal("partial stream content was not preserved")
			}
			if errorMsg != tt.wantError {
				t.Fatalf("saved error = %q, want %q", errorMsg, tt.wantError)
			}
		})
	}
}

func TestProcessClaudeStreamAcceptsMessageStopAndDone(t *testing.T) {
	for _, terminal := range []string{
		"data: {\"type\":\"message_stop\"}",
		"data: [DONE]",
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
		if err := (&AccountTestService{}).processClaudeStream(ctx, strings.NewReader(terminal)); err != nil {
			t.Fatalf("terminal %q returned error: %v", terminal, err)
		}
		_, errorMsg := parseTestSSEOutput(recorder.Body.String())
		if errorMsg != "" {
			t.Fatalf("terminal %q saved error %q", terminal, errorMsg)
		}
	}
}
