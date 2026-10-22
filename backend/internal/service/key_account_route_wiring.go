package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

type keyAccountRouteSiteReader interface {
	GetKeyRouteSiteID(context.Context) (string, error)
}

func ProvideKeyAccountRoutingRuntime(store keyrouting.AccountHealthStore, keys APIKeyRepository, accounts AccountRepository) (*KeyAccountRoutingRuntime, error) {
	revisions, ok := keys.(KeyRouteRevisionReader)
	if !ok {
		return nil, errors.New("API key repository does not support account route revisions")
	}
	sites, ok := keys.(keyAccountRouteSiteReader)
	if !ok {
		return nil, errors.New("API key repository does not support routing site namespaces")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	site, err := sites.GetKeyRouteSiteID(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialize account routing namespace: %w", err)
	}
	return NewKeyAccountRoutingRuntime(store, revisions, accounts, KeyAccountRoutingRuntimeOptions{Site: site}), nil
}

// Production composition extends the existing constructor; tests and native
// single-group integrations keep using NewKeyRoutingService unchanged.
func ProvideKeyRoutingService(groups GroupRepository, rates UserGroupRateRepository, native *GatewayService, openai *OpenAIGatewayService, composite *CompositeRouteResolver, cfg *config.Config, settings *SettingService, accounts *KeyAccountRoutingRuntime) *KeyRoutingService {
	s := NewKeyRoutingService(groups, rates, native, openai, composite, cfg, settings)
	s.accounts = accounts
	return s
}

// PrepareAccountRouteContext is invoked once by ingress, before group
// readiness. Its session survives clean cross-group dispatch contexts.
func (s *KeyRoutingService) PrepareAccountRouteContext(ctx context.Context, key *APIKey, request KeyRoutingRequest) (context.Context, error) {
	if key == nil || key.KeyRouteScope != KeyRouteScopeAccountV2 || len(key.BackupGroupIDs) == 0 {
		return ctx, nil
	}
	if s == nil || s.accounts == nil {
		return ctx, errors.New("account-scoped key routing is unavailable")
	}
	if GetKeyAccountRoutingSession(ctx) != nil {
		return ctx, errors.New("account-scoped key routing session already exists")
	}
	// These are whole-request safety ceilings, not new per-group retry
	// permissions. Native account/group limits continue to apply. In
	// particular, budget exhaustion does not imply candidate exhaustion.
	budget, err := keyrouting.NewAccountRouteBudget(256, 10*time.Hour)
	if err != nil {
		return ctx, err
	}
	session, err := s.accounts.NewSession(ctx, key, request, budget)
	if err != nil {
		return ctx, err
	}
	return WithKeyAccountRoutingSession(ctx, session), nil
}

// AccountGroupHasCandidate reuses the exact planner/readiness adapter for ONE
// group. The coordinator uses it to distinguish a native retry limit from an
// exhausted group; it does not reserve accounts or debit balances.
func (s *KeyRoutingService) AccountGroupHasCandidate(ctx context.Context, key *APIKey, request KeyRoutingRequest, groupID int64) (bool, error) {
	if GetKeyAccountRoutingSession(ctx) == nil || key == nil || groupID <= 0 {
		return false, ErrKeyAccountRouteScope
	}
	probe := *key
	probe.GroupID, probe.Group, probe.BackupGroupIDs = &groupID, nil, nil
	_, _, err := s.selectFrom(ctx, &probe, request, 0, false)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, keyrouting.ErrNoSchedulableAccounts), errors.Is(err, keyrouting.ErrModelUnavailable):
		return false, nil
	default:
		return false, err
	}
}
