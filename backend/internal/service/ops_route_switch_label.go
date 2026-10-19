package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

const (
	// OpsRouteSwitchLabelPrefix is intentionally short: it is shown alongside
	// the existing error message in the operations error list/detail.
	OpsRouteSwitchLabelPrefix  = "切换备用分组："
	maxOpsRouteSwitchGroupName = 128
)

// WithOpsRouteSwitchLabel attaches the backup group name to a request context.
// The value is a diagnostic annotation only; it does not affect routing,
// billing, usage records, or account scheduling.
func WithOpsRouteSwitchLabel(ctx context.Context, groupName string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	label := NormalizeOpsRouteSwitchLabel(groupName)
	if label == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxkey.OpsRouteSwitchLabel, label)
}

// OpsRouteSwitchLabel reads the request-scoped backup-group label.  It returns
// the rendered label (including the Chinese prefix) so callers can pass it
// directly to ApplyOpsRouteSwitchLabel or include it in an error payload.
func OpsRouteSwitchLabel(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw, _ := ctx.Value(ctxkey.OpsRouteSwitchLabel).(string)
	return NormalizeOpsRouteSwitchLabel(raw)
}

// NormalizeOpsRouteSwitchLabel accepts either a group name or a previously
// rendered label.  Keeping normalization here prevents duplicate prefixes
// when a request passes through more than one error-recording path.
func NormalizeOpsRouteSwitchLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, OpsRouteSwitchLabelPrefix)
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = truncateString(value, maxOpsRouteSwitchGroupName)
	return OpsRouteSwitchLabelPrefix + value
}

// ApplyOpsRouteSwitchLabel records the annotation on an error-log input and
// appends it to the existing error message.  Ops error logs intentionally use
// their existing schema; usage records are not changed.  The operation is
// idempotent so both the middleware and persistence preparation can call it.
func ApplyOpsRouteSwitchLabel(entry *OpsInsertErrorLogInput, label string) {
	if entry == nil {
		return
	}
	label = NormalizeOpsRouteSwitchLabel(label)
	if label == "" {
		return
	}
	entry.RouteSwitchLabel = label
	if strings.Contains(entry.ErrorMessage, label) {
		return
	}
	message := strings.TrimSpace(entry.ErrorMessage)
	if message == "" {
		entry.ErrorMessage = label
		return
	}
	entry.ErrorMessage = message + " · " + label
}
