package service

import (
	"context"
	"net/http"
	"reflect"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// moshuResellerSyncContextKey is deliberately private.  Only the reseller
// synchronization package can opt into updating fields owned by the parent
// station; HTTP handlers never copy this marker from a request.
type moshuResellerSyncContextKey struct{}

// WithMoshuResellerSync marks an internal catalog reconciliation operation.
// It is not a user-facing authorization mechanism and must only be used by
// the moshureseller package after the upstream product has been authenticated.
func WithMoshuResellerSync(ctx context.Context) context.Context {
	return context.WithValue(ctx, moshuResellerSyncContextKey{}, true)
}

func isMoshuResellerSync(ctx context.Context) bool {
	allowed, _ := ctx.Value(moshuResellerSyncContextKey{}).(bool)
	return allowed
}

func moshuResellerMutationError(field string) error {
	return infraerrors.Newf(http.StatusForbidden, "MOSHU_RESELLER_MANAGED_ACCOUNT", "reseller-managed account field %s can only be changed by upstream synchronization", field)
}

// validateMoshuResellerManagedUpdate keeps the parent station authoritative
// for protocol identity, credentials and account model mappings.  Unrelated
// operational fields (name, status, capacity, etc.) remain editable by an
// administrator.  A full-object PUT that echoes an unchanged credential is
// harmless and is accepted; a changed value is rejected.
func validateMoshuResellerManagedUpdate(ctx context.Context, account *Account, input *UpdateAccountInput) error {
	if account == nil || !account.IsMoshuResellerManaged() || isMoshuResellerSync(ctx) {
		return nil
	}
	if input == nil {
		return nil
	}
	if input.Platform != "" && input.Platform != account.Platform {
		return moshuResellerMutationError("platform")
	}
	if input.Type != "" && input.Type != account.Type {
		return moshuResellerMutationError("type")
	}
	if len(input.Credentials) > 0 {
		for key, incoming := range input.Credentials {
			existing, exists := account.Credentials[key]
			if !exists || !reflect.DeepEqual(existing, incoming) {
				return moshuResellerMutationError("credentials." + key)
			}
		}
	}
	if input.GroupIDs != nil {
		return moshuResellerMutationError("group_ids")
	}
	if hasMoshuResellerOwnedExtraMutation(input.Extra) {
		return moshuResellerMutationError("extra")
	}
	return nil
}

func hasMoshuResellerOwnedExtraMutation(updates map[string]any) bool {
	for key := range updates {
		if key == MoshuResellerModelSnapshotExtraKey || key == MoshuResellerPassthroughExtraKey || key == "moshu_reseller_managed" || key == "moshu_product_code" || key == "moshu_remote_product_id" || key == "moshu_cost_read_only" || key == "openai_passthrough" || key == "anthropic_passthrough" {
			return true
		}
	}
	return false
}

func validateMoshuResellerManagedBulkUpdate(ctx context.Context, accounts []*Account, input *BulkUpdateAccountsInput) error {
	if input == nil || isMoshuResellerSync(ctx) {
		return nil
	}
	for _, account := range accounts {
		if account == nil || !account.IsMoshuResellerManaged() {
			continue
		}
		if len(input.Credentials) > 0 {
			return moshuResellerMutationError("credentials")
		}
		if input.GroupIDs != nil {
			return moshuResellerMutationError("group_ids")
		}
		if hasMoshuResellerOwnedExtraMutation(input.Extra) {
			return moshuResellerMutationError("extra")
		}
	}
	return nil
}
