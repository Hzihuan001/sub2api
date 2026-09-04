package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
)

const operatorRolePolicyCacheTTL = 15 * time.Second

type cachedOperatorRolePolicy struct {
	policy    authz.OperatorPolicy
	expiresAt time.Time
}

func (s *SettingService) GetOperatorRolePolicy(ctx context.Context) (authz.OperatorPolicy, error) {
	defaults := authz.DefaultOperatorPolicy()
	if s == nil || s.settingRepo == nil {
		return defaults, nil
	}

	raw, err := s.settingRepo.GetValue(ctx, SettingKeyOperatorRolePolicy)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return defaults, nil
		}
		return defaults, fmt.Errorf("get operator role policy: %w", err)
	}

	var policy authz.OperatorPolicy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return defaults, fmt.Errorf("decode operator role policy: %w", err)
	}
	return authz.NormalizeOperatorPolicy(policy), nil
}

// GetOperatorRolePolicyCached is used on the management authorization hot
// path. It never fails a request because of a transient settings-store error:
// the last good value is retained, or the safe default is used on cold start.
func (s *SettingService) GetOperatorRolePolicyCached(ctx context.Context) authz.OperatorPolicy {
	defaults := authz.DefaultOperatorPolicy()
	if s == nil || s.settingRepo == nil {
		return defaults
	}
	if cached, ok := s.operatorRolePolicyCache.Load().(*cachedOperatorRolePolicy); ok && time.Now().Before(cached.expiresAt) {
		return cached.policy
	}

	value, err, _ := s.operatorRolePolicySF.Do("operator_role_policy", func() (any, error) {
		policy, loadErr := s.GetOperatorRolePolicy(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		s.operatorRolePolicyCache.Store(&cachedOperatorRolePolicy{
			policy:    policy,
			expiresAt: time.Now().Add(operatorRolePolicyCacheTTL),
		})
		return policy, nil
	})
	if err == nil {
		return value.(authz.OperatorPolicy)
	}
	if cached, ok := s.operatorRolePolicyCache.Load().(*cachedOperatorRolePolicy); ok {
		return cached.policy
	}
	return authz.FailClosedOperatorPolicy()
}

func (s *SettingService) SetOperatorRolePolicy(ctx context.Context, policy authz.OperatorPolicy) (authz.OperatorPolicy, error) {
	if s == nil || s.settingRepo == nil {
		return authz.OperatorPolicy{}, fmt.Errorf("operator role policy store is unavailable")
	}
	normalized := authz.NormalizeOperatorPolicy(policy)
	raw, err := json.Marshal(normalized)
	if err != nil {
		return authz.OperatorPolicy{}, fmt.Errorf("encode operator role policy: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyOperatorRolePolicy, string(raw)); err != nil {
		return authz.OperatorPolicy{}, fmt.Errorf("save operator role policy: %w", err)
	}
	s.operatorRolePolicyCache.Store(&cachedOperatorRolePolicy{
		policy:    normalized,
		expiresAt: time.Now().Add(operatorRolePolicyCacheTTL),
	})
	return normalized, nil
}
