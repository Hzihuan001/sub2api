package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

func (s *KeyRoutingService) FirstContentTimeout(ctx context.Context, key *APIKey, _ KeyRoutingRequest, groupID int64) (time.Duration, error) {
	if s == nil || key == nil {
		return 0, nil
	}
	policy, err := s.loadRouteFailurePolicy(ctx, key, groupID)
	if err != nil {
		return 0, err
	}
	return policy.FirstContentTimeout(), nil
}

func (s *KeyRoutingService) RecordFirstContent(ctx context.Context, key *APIKey, request KeyRoutingRequest, groupID int64, elapsed time.Duration) (keyrouting.RouteFailureResult, error) {
	if s == nil || s.failures == nil || key == nil {
		return keyrouting.RouteFailureResult{}, nil
	}
	policy, err := s.loadRouteFailurePolicy(ctx, key, groupID)
	if err != nil {
		return keyrouting.RouteFailureResult{}, err
	}
	return s.failures.RecordFirstContent(keyrouting.RouteFailureIdentity{APIKeyID: key.ID, GroupID: groupID, Model: request.Model, Endpoint: request.Endpoint}, elapsed, policy), nil
}
