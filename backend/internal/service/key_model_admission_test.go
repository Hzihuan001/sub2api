package service

import (
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKeyModelPolicyWSFrameOriginalCandidatesAndSessions(t *testing.T) {
	for _, tc := range []struct {
		name, payload, fallback string
		allowed                 bool
	}{
		{"first allowed", `{"type":"response.create","model":"allowed"}`, "", true},
		{"first blocked", `{"type":"response.create","model":"blocked"}`, "", false},
		{"missing", `{"type":"response.create"}`, "", false},
		{"omitted uses session", `{"type":"response.create"}`, "allowed", true},
		{"blocked effective session", `{"type":"response.create","session":{"model":"allowed"}}`, "blocked", false},
		{"explicit overrides old session", `{"type":"response.create","model":"allowed"}`, "blocked", true},
		{"duplicate", `{"type":"response.create","model":"allowed","model":"blocked"}`, "", false},
		{"case variant", `{"type":"response.create","model":"allowed","Model":"blocked"}`, "", false},
		{"null override", `{"type":"response.create","model":"allowed","model":null}`, "", false},
		{"empty override", `{"type":"response.create","model":"allowed","model":""}`, "", false},
		{"null session model", `{"type":"session.update","session":{"model":null}}`, "allowed", false},
		{"session immediate deny", `{"type":"session.update","session":{"model":"blocked"}}`, "allowed", false},
		{"session duplicate", `{"type":"session.update","session":{"model":"allowed","Model":"blocked"}}`, "allowed", false},
		{"top cannot hide session", `{"type":"session.update","model":"allowed","session":{"model":"blocked"}}`, "allowed", false},
		{"allowed session", `{"type":"session.update","session":{"model":"allowed"}}`, "allowed", true},
		{"model-free session", `{"type":"session.update","session":{"voice":"alloy"}}`, "allowed", true},
		{"cancel unaffected", `{"type":"response.cancel"}`, "allowed", true},
		{"invalid JSON", `{`, "allowed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("api_key", &APIKey{KeyModelPolicy: &KeyModelPolicy{Enabled: true, Models: []string{"allowed"}}})
			err := CheckKeyModelPolicyWSFrame(c, []byte(tc.payload), tc.fallback)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				var closeErr *OpenAIWSClientCloseError
				require.ErrorAs(t, err, &closeErr)
				require.Equal(t, websocket.StatusPolicyViolation, closeErr.StatusCode())
			}
		})
	}
}

func TestKeyModelPolicyWSFrameAbsentOrDisabledDoesNotInterfere(t *testing.T) {
	require.NoError(t, CheckKeyModelPolicyWSFrame(nil, []byte(`{`), ""))
	for _, key := range []*APIKey{nil, {}, {KeyModelPolicy: &KeyModelPolicy{Enabled: false, Models: []string{"allowed"}}}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("api_key", key)
		require.NoError(t, CheckKeyModelPolicyWSFrame(c, []byte(`{"type":"response.create","model":"blocked"}`), ""))
	}
}
