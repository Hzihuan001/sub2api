package securityaudit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareEventForListRebuildsLegacyCaptureOnlyPreview(t *testing.T) {
	event := &Event{
		CaptureMode: "capture_only",
		Snapshot: PromptSnapshot{
			RedactedPreview: "***",
			FullPrompt:      "请总结这段中文，并联系 email@example.com",
		},
	}

	prepareEventForList(event)

	require.Contains(t, event.Snapshot.RedactedPreview, "请总结这段中文")
	require.NotContains(t, event.Snapshot.RedactedPreview, "email@example.com")
	require.Contains(t, event.Snapshot.RedactedPreview, "***@***")
	require.Empty(t, event.Snapshot.FullPrompt)
}

func TestPrepareEventForListPreservesExistingPreviewAndClearsFullPrompt(t *testing.T) {
	tests := []struct {
		name        string
		captureMode string
	}{
		{name: "current capture-only row", captureMode: "capture_only"},
		{name: "guard event", captureMode: "guard_audit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &Event{
				CaptureMode: tt.captureMode,
				Snapshot: PromptSnapshot{
					RedactedPreview: "already sanitized",
					FullPrompt:      "must not leave the list response",
				},
			}

			prepareEventForList(event)

			require.Equal(t, "already sanitized", event.Snapshot.RedactedPreview)
			require.Empty(t, event.Snapshot.FullPrompt)
		})
	}
}
