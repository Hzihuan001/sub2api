package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

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
