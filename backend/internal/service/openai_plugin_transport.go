package service

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account, actualModel ...string) (response *http.Response, err error) {
	if request == nil {
		return nil, errors.New("OpenAI inference request is nil")
	}
	// A scoped inference must supply its final native mapping. Never guess it
	// from a public alias, and never count the plugin and HTTP fallback twice.
	if KeyAccountForwardScopeFromContext(request.Context()) == nil {
		return s.doOpenAITransport(request, proxyURL, account)
	}
	if len(actualModel) > 0 {
		// Reserve only the runtime, not an inference permit. Local readiness
		// failures must not manufacture actual-dispatch health samples.
		reservation, handled, reserveErr := s.pluginManager.reserveOpenAIOAuth(account)
		if reserveErr != nil {
			return nil, reserveErr
		}
		if handled {
			defer reservation.cancelBeforeDispatch()
		}
		return KeyAccountInferenceDoFunc(request, account, actualModel[0], func(wire *http.Request) (*http.Response, error) {
			var response *http.Response
			var err error
			if handled {
				response, err = reservation.roundTrip(wire.Context(), wire, proxyURL, account)
			} else {
				response, err = s.httpUpstream.Do(wire, proxyURL, account.ID, account.Concurrency)
			}
			// Only an explicit RequestSent=false proves no upstream submission.
			// Cancellation can erase the native typed error, so an untyped
			// plugin failure is also ambiguous and must keep no-replay safety.
			var pluginErr *PluginTransportError
			if handled && err != nil && (!errors.As(err, &pluginErr) || pluginErr.RequestSent) {
				if attempt := keyrouting.GetAccountAttempt(wire.Context()); attempt != nil {
					attempt.PreventReplay(keyrouting.AttemptVetoNativeGuard)
				}
			}
			return response, err
		})
	}
	return nil, fmt.Errorf("%w: missing native inference model", ErrKeyAccountRouteScope)
}

// Non-inference calls explicitly retain their native transport without
// creating account-health samples. Do not use this port for generation.
func (s *OpenAIGatewayService) doOpenAINonInferenceUpstream(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	return s.doOpenAITransport(request, proxyURL, account)
}

func (s *OpenAIGatewayService) doOpenAITransport(request *http.Request, proxyURL string, account *Account) (response *http.Response, err error) {
	defer func() {
		if err == nil {
			protectKeyRouteUsageResponse(request.Context(), response)
		}
	}()
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	if useTLSFallback {
		return s.httpUpstream.DoWithTLS(
			request,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(account),
		)
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}
