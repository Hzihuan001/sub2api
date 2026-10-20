package service

import (
	"context"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ValidateKeyRouteWriteMetadata rejects client-controlled generations. Scope
// omission preserves legacy semantics; only an explicit supported value opts in.
func ValidateKeyRouteWriteMetadata(scope *string, revision []byte) error {
	if len(revision) != 0 {
		return infraerrors.BadRequest("API_KEY_ROUTE_REVISION_READ_ONLY", "key_route_revision is managed by the server")
	}
	if scope != nil && *scope != KeyRouteScopeGroupV1 && *scope != KeyRouteScopeAccountV2 {
		return infraerrors.BadRequest("API_KEY_ROUTE_SCOPE_INVALID", "key_route_scope must be group_v1 or account_v2")
	}
	return nil
}

func keyRouteWriteScope(scope *string) string {
	if scope == nil {
		return KeyRouteScopeGroupV1
	}
	return *scope
}

const (
	KeyRouteScopeGroupV1   = "group_v1"
	KeyRouteScopeAccountV2 = "account_v2"
)

// KeyRouteRevisionReader is an optional extension of the upstream repository.
// Group payload and generation must be read in the same database snapshot.
// Empty raw means the group has no persisted rule; its revision is still kept
// after deletion so re-enabling identical rules cannot revive old counters.
type KeyRouteRevisionReader interface {
	GetKeyRouteRevision(context.Context, int64) (int64, error)
	GetGroupRoutePolicySnapshot(context.Context, int64) (raw string, revision int64, err error)
}

// Empty scope is accepted only as the historical group_v1 zero value used by
// existing callers. Persisted account_v2 metadata always requires a generation.
func ValidateKeyRouteMetadata(scope string, revision int64) error {
	if revision < 0 {
		return fmt.Errorf("API key route revision cannot be negative")
	}
	switch scope {
	case "", KeyRouteScopeGroupV1:
		return nil
	case KeyRouteScopeAccountV2:
		if revision > 0 {
			return nil
		}
		return fmt.Errorf("account_v2 API key routes require a positive revision")
	default:
		return fmt.Errorf("unsupported API key route scope %q", scope)
	}
}
