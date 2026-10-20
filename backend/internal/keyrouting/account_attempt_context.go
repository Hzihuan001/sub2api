package keyrouting

import "context"

// FirstContentGuardProvider is used only by an explicitly installed native
// Forward scope. It lets native parsers/writers resolve the current wire
// attempt without retaining the preceding account's guard.
type FirstContentGuardProvider interface {
	CurrentFirstContentGuard() *FirstContentGuard
}

type firstContentGuardProviderContextKey struct{}
type firstContentGuardProviderBinding struct{ provider FirstContentGuardProvider }

func WithFirstContentGuardProvider(ctx context.Context, provider FirstContentGuardProvider) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, firstContentGuardProviderContextKey{}, firstContentGuardProviderBinding{provider: provider})
}

func boundFirstContentGuardProvider(ctx context.Context) FirstContentGuardProvider {
	if ctx == nil {
		return nil
	}
	binding, _ := ctx.Value(firstContentGuardProviderContextKey{}).(firstContentGuardProviderBinding)
	return binding.provider
}

// A captured wire context must remain bound to its own guard even after the
// Forward scope advances. Never let a late reader consult the next attempt.
func withoutFirstContentGuardProvider(ctx context.Context) context.Context {
	return context.WithValue(ctx, firstContentGuardProviderContextKey{}, firstContentGuardProviderBinding{})
}
