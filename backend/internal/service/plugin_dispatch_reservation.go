package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
)

var errOpenAIPluginReservationUsed = errors.New("OpenAI OAuth plugin dispatch reservation is no longer available")

const (
	openAIPluginReserved uint32 = iota
	openAIPluginDispatching
	openAIPluginResponseOwned
	openAIPluginReleased
)

// openAIPluginReservation separates local permission/readiness admission from
// inference transport dispatch. It retains the same runtime that admission
// accepted; draining waits for this in-flight reservation just as it waits for
// a request already accepted by RoundTripOpenAIOAuth.
//
// The caller must defer cancelBeforeDispatch. A successful roundTrip transfers
// release ownership to the unchanged pluginResponseBody; errors release here.
type openAIPluginReservation struct {
	manager *PluginManager
	route   *pluginRoute
	state   atomic.Uint32
}

func shouldRouteOpenAIOAuthSnapshot(route *pluginRoute, account *Account) bool {
	return account != nil && account.Platform == PlatformOpenAI && account.Type == AccountTypeOAuth &&
		route != nil && route.rolloutPercent > 0 && int(stablePluginBucket(account.ID)) < route.rolloutPercent
}

// reserveOpenAIOAuth is the sole local route/readiness check. It must run before
// a scoped inference spends its dispatch permit or creates an attempt. A single
// immutable route snapshot supplies both the permission decision and runtime.
func (m *PluginManager) reserveOpenAIOAuth(account *Account) (*openAIPluginReservation, bool, error) {
	if m == nil {
		return nil, false, nil
	}
	route := m.route.Load()
	if !shouldRouteOpenAIOAuthSnapshot(route, account) {
		return nil, false, nil
	}
	if route.runtime == nil {
		return nil, true, fmt.Errorf("OpenAI OAuth 插件不可用: %s", route.unavailable)
	}
	if route.runtime.client == nil || route.runtime.api == nil {
		return nil, true, errors.New("OpenAI OAuth 插件运行时不完整")
	}
	if route.runtime.client.Exited() {
		runtimeErr := errors.New("OpenAI OAuth 插件进程已退出")
		if stateErr := m.markRuntimeUnavailable(route, runtimeErr.Error()); stateErr != nil {
			return nil, true, errors.Join(runtimeErr, stateErr)
		}
		return nil, true, runtimeErr
	}
	if !route.runtime.beginRequest() {
		return nil, true, errors.New("OpenAI OAuth 插件正在停止")
	}
	return &openAIPluginReservation{manager: m, route: route}, true, nil
}

func (r *openAIPluginReservation) cancelBeforeDispatch() bool {
	if r == nil || !r.state.CompareAndSwap(openAIPluginReserved, openAIPluginReleased) {
		return false
	}
	r.route.runtime.finishRequest()
	return true
}

func (r *openAIPluginReservation) roundTrip(ctx context.Context, request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if r == nil || !r.state.CompareAndSwap(openAIPluginReserved, openAIPluginDispatching) {
		return nil, errOpenAIPluginReservationUsed
	}
	response, err := r.route.runtime.roundTrip(ctx, request, proxyURL, account)
	if err != nil {
		r.state.Store(openAIPluginReleased)
		r.route.runtime.finishRequest()
		if r.route.runtime.client.Exited() {
			if stateErr := r.manager.markRuntimeUnavailable(r.route, err.Error()); stateErr != nil {
				err = errors.Join(err, stateErr)
			}
		}
		return nil, err
	}
	// The native pluginResponseBody.Close owns exactly one finishRequest from
	// here. An abandoned or repeated reservation cancellation cannot release it.
	r.state.Store(openAIPluginResponseOwned)
	return response, nil
}
