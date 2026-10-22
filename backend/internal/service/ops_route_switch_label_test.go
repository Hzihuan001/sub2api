package service

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMarkOpsRouteSwitchAnnotatesOnlyCurrentAttemptError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(OpsUpstreamErrorsKey, []*OpsUpstreamErrorEvent{{AccountID: 10, UpstreamStatusCode: 503, Message: "primary failure"}})
	BeginOpsRouteErrorAttempt(c, 20)
	c.Set(OpsUpstreamErrorsKey, []*OpsUpstreamErrorEvent{
		{AccountID: 10, UpstreamStatusCode: 503, Message: "primary failure"},
		{AccountID: 20, UpstreamStatusCode: 502, Message: "causal failure"},
	})
	MarkOpsRouteSwitch(c, "备用分组")
	events, _ := c.Get(OpsUpstreamErrorsKey)
	items := events.([]*OpsUpstreamErrorEvent)
	require.Empty(t, items[0].RouteSwitchGroup)
	require.Equal(t, "备用分组", items[1].RouteSwitchGroup)
	require.Equal(t, OpsRouteSwitchLabelPrefix+"备用分组", LastOpsRouteSwitchLabel(items))

	// A later attempt that fails without switching must suppress the row-level
	// summary annotation while preserving the earlier causal event.
	c.Set(OpsUpstreamErrorsKey, append(items, &OpsUpstreamErrorEvent{AccountID: 30, UpstreamStatusCode: 500, Message: "later failure"}))
	BeginOpsRouteErrorAttempt(c, 30)
	MarkOpsRouteSwitch(c, "ignored") // no new event was added in this attempt
	events, _ = c.Get(OpsUpstreamErrorsKey)
	items = events.([]*OpsUpstreamErrorEvent)
	require.Empty(t, items[2].RouteSwitchGroup)
	require.Empty(t, LastOpsRouteSwitchLabel(items))
	require.Equal(t, "备用分组", items[1].RouteSwitchGroup)
}

func TestMarkOpsRouteSwitchDoesNotInventOrRelabelACause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		begin       bool
		accountID   int64
		appendEvent bool
	}{
		{name: "no attempt boundary"},
		{name: "empty group", begin: true, accountID: 20},
		{name: "different account", begin: true, accountID: 20, appendEvent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			prior := &OpsUpstreamErrorEvent{AccountID: 10, Message: "previous error", RouteSwitchGroup: "original target"}
			c.Set(OpsUpstreamErrorsKey, []*OpsUpstreamErrorEvent{prior})
			if tc.begin {
				BeginOpsRouteErrorAttempt(c, tc.accountID)
			}
			if tc.appendEvent {
				c.Set(OpsUpstreamErrorsKey, []*OpsUpstreamErrorEvent{prior, {AccountID: 30, Message: "other account"}})
			}
			MarkOpsRouteSwitch(c, "new target")
			items := c.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
			require.Equal(t, "original target", items[0].RouteSwitchGroup)
			if tc.appendEvent {
				require.Empty(t, items[1].RouteSwitchGroup)
			}
		})
	}
	BeginOpsRouteErrorAttempt(nil, 1)
	MarkOpsRouteSwitch(nil, "target")
}

func TestMarkOpsRouteSwitchCopiesSnapshotsAndConsumesCause(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	BeginOpsRouteErrorAttempt(c, 41)
	old := []*OpsUpstreamErrorEvent{{AccountID: 41, Message: "overloaded"}}
	c.Set(OpsUpstreamErrorsKey, old)
	MarkOpsRouteSwitch(c, "second")
	items := c.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
	require.Empty(t, old[0].RouteSwitchGroup, "queued snapshots must not be mutated")
	require.Equal(t, "second", items[0].RouteSwitchGroup)
	MarkOpsRouteSwitch(c, "third")
	items = c.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
	require.Equal(t, "second", items[0].RouteSwitchGroup, "an empty intermediate group cannot steal the cause")
}

func TestOpsRouteSwitchEventSurvivesQueueSerialization(t *testing.T) {
	original := &OpsUpstreamErrorEvent{AccountID: 41, UpstreamStatusCode: 503, Message: "overloaded", RouteSwitchGroup: "  " + strings.Repeat("x", maxOpsRouteSwitchGroupName+40) + "  "}
	entry := &OpsInsertErrorLogInput{UpstreamErrors: []*OpsUpstreamErrorEvent{original}}
	require.NoError(t, sanitizeOpsUpstreamErrors(entry))
	require.Nil(t, entry.UpstreamErrors)
	require.NotNil(t, entry.UpstreamErrorsJSON)
	events, err := ParseOpsUpstreamErrors(*entry.UpstreamErrorsJSON)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, strings.Repeat("x", maxOpsRouteSwitchGroupName), events[0].RouteSwitchGroup)
	require.Equal(t, 503, events[0].UpstreamStatusCode)
	require.Greater(t, len(original.RouteSwitchGroup), maxOpsRouteSwitchGroupName, "sanitization must not mutate the original")
}

func TestOpsRouteSwitchLabelContextAndApply(t *testing.T) {
	ctx := WithOpsRouteSwitchLabel(context.Background(), "  备用分组  ")
	require.Equal(t, "切换备用分组：备用分组", OpsRouteSwitchLabel(ctx))

	entry := &OpsInsertErrorLogInput{ErrorMessage: "upstream request failed"}
	ApplyOpsRouteSwitchLabel(entry, OpsRouteSwitchLabel(ctx))
	require.Equal(t, "切换备用分组：备用分组", entry.RouteSwitchLabel)
	require.Equal(t, "upstream request failed · 切换备用分组：备用分组", entry.ErrorMessage)

	// Preparation/middleware can both apply the label without duplicating it.
	ApplyOpsRouteSwitchLabel(entry, OpsRouteSwitchLabel(ctx))
	require.Equal(t, "upstream request failed · 切换备用分组：备用分组", entry.ErrorMessage)
}

func TestOpsRouteSwitchLabelNormalizesAndBoundsGroupName(t *testing.T) {
	require.Empty(t, OpsRouteSwitchLabel(nil))
	require.Empty(t, NormalizeOpsRouteSwitchLabel("切换备用分组："))

	longName := make([]byte, maxOpsRouteSwitchGroupName+40)
	for i := range longName {
		longName[i] = 'x'
	}
	label := NormalizeOpsRouteSwitchLabel(string(longName))
	require.Len(t, label, len(OpsRouteSwitchLabelPrefix)+maxOpsRouteSwitchGroupName)
	require.Equal(t, OpsRouteSwitchLabelPrefix, label[:len(OpsRouteSwitchLabelPrefix)])
}
