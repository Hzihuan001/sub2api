package service

import "context"

type resellerResourceProductKey struct{}

// Internal-only marker for atomic resource creation and recovery linkage.
func WithResellerResourceProduct(ctx context.Context, productID int64) context.Context {
	return context.WithValue(ctx, resellerResourceProductKey{}, productID)
}

func ResellerResourceProduct(ctx context.Context) int64 {
	id, _ := ctx.Value(resellerResourceProductKey{}).(int64)
	return id
}
