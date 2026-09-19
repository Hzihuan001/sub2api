package moshureseller

// productAccountGroupIDs returns the account's existing group bindings with
// the product's current local group guaranteed to be present.
//
// Reseller-managed accounts can still have administrator-managed bindings.
// There is no durable marker on an account_groups row that distinguishes a
// historical reseller product group from such a manual binding, so sync must
// not remove existing rows speculatively. The product group is authoritative
// for the current product and is therefore added when it is missing; cleanup
// of stale reseller-owned rows requires explicit binding metadata.
func productAccountGroupIDs(existing []int64, productGroupID *int64) []int64 {
	groupIDs := append([]int64(nil), existing...)
	if productGroupID == nil {
		return groupIDs
	}
	for _, groupID := range groupIDs {
		if groupID == *productGroupID {
			return groupIDs
		}
	}
	return append(groupIDs, *productGroupID)
}
