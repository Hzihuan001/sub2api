package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/gin-gonic/gin"
)

func TestKeyRouteFirstContentDetachReleaseDoesNotCancelUpstream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		guard := keyrouting.NewFirstContentGuard(time.Hour)
		root := keyrouting.WithFirstContentGuard(context.Background(), guard)
		child, release := detachStreamUpstreamContext(root, stream)
		release() // Native builders call this before their actual HTTP Do.
		if child.Err() != nil || root.Err() != nil {
			t.Fatalf("legacy release prematurely canceled transport, stream=%v", stream)
		}
		guard.Finish()
		if child.Err() != context.Canceled || root.Err() != nil {
			t.Fatal("only guard-owned child should be canceled by Finish")
		}
	}
	guard := keyrouting.NewFirstContentGuard(time.Hour)
	root := keyrouting.WithFirstContentGuard(context.Background(), guard)
	child, release := detachUpstreamContext(root)
	release()
	if child.Err() != nil {
		t.Fatal("detached request-builder release canceled the transport")
	}
	guard.Finish()
	if child.Err() != context.Canceled {
		t.Fatal("guard did not retain detached transport cancellation ownership")
	}
}

func TestKeyRouteFirstContentDoesNotChangeUnguardedDetachOrBilling(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	syncCtx, releaseSync := detachStreamUpstreamContext(root, false)
	streamCtx, releaseStream := detachStreamUpstreamContext(root, true)
	detached, releaseDetached := detachUpstreamContext(root)
	releaseSync()
	releaseStream()
	releaseDetached()
	cancel()
	if syncCtx != root || syncCtx.Err() != context.Canceled || streamCtx.Err() != nil || detached.Err() != nil {
		t.Fatal("unguarded upstream cancellation behavior changed")
	}
	guard := keyrouting.NewFirstContentGuard(time.Millisecond)
	guarded := keyrouting.WithFirstContentGuard(context.Background(), guard)
	billing, finishBilling := detachedBillingContext(guarded)
	defer finishBilling()
	child, release := detachUpstreamContext(guarded)
	release()
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("guard did not expire")
	}
	guard.Finish()
	if billing.Err() != nil || guarded.Err() != nil {
		t.Fatal("route deadline canceled billing or inbound request")
	}
}

func TestKeyRouteFirstContentDeadlineBypassesTransportHealthClassification(t *testing.T) {
	guard := keyrouting.NewFirstContentGuard(time.Millisecond)
	ctx := keyrouting.WithFirstContentGuard(context.Background(), guard)
	child, release := detachUpstreamContext(ctx)
	release()
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("guard did not expire")
	}
	defer guard.Finish()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	err := errors.New("persistent network-looking failure from canceled attempt")
	// Nil services/accounts intentionally prove the guard returns before any
	// native scheduler, account, proxy, or Ops-health mutation is attempted.
	if got := (*OpenAIGatewayService)(nil).handleOpenAIUpstreamTransportError(ctx, c, nil, err, false); got != err {
		t.Fatal("OpenAI replaced user deadline with provider failure")
	}
	if got := (*GatewayService)(nil).handleUpstreamTransportError(ctx, c, nil, err, OpsUpstreamErrorEvent{}); got != err {
		t.Fatal("Anthropic replaced user deadline with provider failure")
	}
	if got := (*GeminiMessagesCompatService)(nil).handleUpstreamTransportError(ctx, c, nil, err); got != err {
		t.Fatal("Gemini replaced user deadline with provider failure")
	}
}

func TestKeyRouteFirstContentDeadlineInterruptsHTTPHeadersAndBodyRead(t *testing.T) {
	for _, sendHeaders := range []bool{false, true} {
		name := "waiting_headers"
		if sendHeaders {
			name = "waiting_body"
		}
		t.Run(name, func(t *testing.T) {
			upstreamDone := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(upstreamDone)
				if sendHeaders {
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer upstream.Close()
			guard := keyrouting.NewFirstContentGuard(100 * time.Millisecond)
			defer guard.Finish()
			root := keyrouting.WithFirstContentGuard(context.Background(), guard)
			ctx, release := detachUpstreamContext(root)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			release()
			resp, err := upstream.Client().Do(req)
			if sendHeaders {
				if err != nil {
					t.Fatalf("headers should arrive before deadline: %v", err)
				}
				_, err = io.ReadAll(resp.Body)
				_ = resp.Body.Close()
			}
			if !errors.Is(err, context.Canceled) || !guard.ReplaySafe() || root.Err() != nil {
				t.Fatalf("deadline did not safely interrupt upstream: err=%v replay=%v root=%v", err, guard.ReplaySafe(), root.Err())
			}
			select {
			case <-upstreamDone:
			case <-time.After(2 * time.Second):
				t.Fatal("canceled upstream handler did not exit")
			}
		})
	}
}

func TestKeyRouteFirstContentRawUsageSurvivesNativeDiscardedPartialResult(t *testing.T) {
	payload := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_usage_prefix\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       &streamReadCloser{payload: []byte(payload), err: context.Canceled},
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}, httpUpstream: upstream}
	guard := keyrouting.NewFirstContentGuard(20 * time.Millisecond)
	defer guard.Finish()
	ctx := keyrouting.WithFirstContentGuard(context.Background(), guard)
	body := []byte(`{"model":"gpt-5.5","stream":true,"input":"hello"}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
	account := &Account{
		ID: 1, Name: "synthetic-usage-prefix", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"access_token": "synthetic", "chatgpt_account_id": "synthetic"},
	}
	result, err := svc.Forward(ctx, c, account, body)
	if err == nil || result != nil {
		t.Fatalf("fixture must exercise native nil-result error boundary, got result=%v err=%v", result, err)
	}
	// Forward's native first-output stage never committed its metadata prefix,
	// and its error path discarded the partial usage result. The raw observer
	// must already have disabled a second attempt independently of both facts.
	time.Sleep(30 * time.Millisecond)
	if guard.Expired() || guard.ReplaySafe() || ctx.Err() != nil {
		t.Fatal("metadata usage was lost when native Forward discarded its partial result")
	}
}
