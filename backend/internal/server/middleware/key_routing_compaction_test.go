package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyRoutingCompactionIntentUsesOnlyProtocolPositions(t *testing.T) {
	for _, body := range []string{
		`{"input":[{"type":"compaction_trigger"}]}`,
		`{"Input":[{"Type":"compaction_trigger"}]}`,
		`{"input":[{"type":"message","content":"hello"},{"type":"compaction_trigger"}]}`,
	} {
		require.True(t, keyRoutingHasPrimaryOnlyIntent([]byte(body)), body)
	}
	for _, body := range []string{
		`{"input":"compaction_trigger"}`,
		`{"input":[{"type":"message","content":"compaction_trigger"}]}`,
		`{"input":[{"type":"message","content":[{"type":"text","text":"compaction_trigger"}]}]}`,
		`{"tools":[{"type":"function","name":"f","parameters":{"type":"object","properties":{"type":{"const":"compaction_trigger"}}}}]}`,
	} {
		require.False(t, keyRoutingHasPrimaryOnlyIntent([]byte(body)), body)
	}
}
