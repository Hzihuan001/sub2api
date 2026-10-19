package service

import (
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Keep user-facing key routing policy separate from administrator rules that
// suspend accounts. The JSON lives in the existing custom extension table.
type KeyRouteFailurePolicy = keyrouting.RouteFailurePolicy
type KeyRouteFailureRule = keyrouting.RouteFailureRule

func validateKeyRouteFailurePolicy(policy *KeyRouteFailurePolicy) error {
	if err := policy.Validate(); err != nil {
		return infraerrors.BadRequest("KEY_ROUTE_FAILURE_POLICY_INVALID", err.Error())
	}
	return nil
}

func cloneAPIKeyRouteFailurePolicy(policy *KeyRouteFailurePolicy) *KeyRouteFailurePolicy {
	return policy.Clone()
}
