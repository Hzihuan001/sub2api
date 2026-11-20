package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/imagerelay"
	"github.com/gin-gonic/gin"
)

// This loopback test exercises the relay components over actual HTTP
// connections. The provider and charge records are fixtures.
func TestImageRelayHTTPChain(t *testing.T) {
	const path = "/v1/images/generations"
	const accountID = int64(11)
	const mainKeyID = int64(22)
	const requestBody = "{\"model\":\"gpt-image-2\",\"prompt\":\"fixture\",\"size\":\"1024x1024\",\"response_format\":\"b64_json\"}"
	const asyncPath = path + "/async"

	var pngBody bytes.Buffer
	if err := png.Encode(&pngBody, image.NewGray(image.Rect(0, 0, 2048, 2048))); err != nil {
		t.Fatal(err)
	}
	base64Image := base64.StdEncoding.EncodeToString(pngBody.Bytes())
	providerBody := []byte("{\"data\":[{\"b64_json\":\"" + base64Image +
		"\",\"size\":\"1024x1024\"}],\"usage\":{\"input_tokens\":6,\"output_tokens\":20}}")
	normalized := reconcileOpenAIImagesAPIResponseSizes(providerBody)

	for _, tc := range []struct {
		name              string
		editRequest       func(*http.Request)
		editResponse      func(*http.Response)
		wantMainCode      int
		wantClientCode    int
		wantProviderCalls int64
	}{
		{"verified delivery and replay protection", nil, nil, 200, 200, 1},
		{"signature tampered", func(r *http.Request) { r.Header.Set(imagerelay.RequestHeader, "invalid") }, nil, 403, 502, 0},
		{"truncated payload", nil, func(r *http.Response) {
			r.Body = prefixedImageRelayBody{ReadCloser: r.Body, reader: io.LimitReader(r.Body, r.ContentLength-1)}
		}, 200, 502, 1},
		{"incompatible response format", nil, func(r *http.Response) { r.Header.Set("Content-Type", "image/png") }, 200, 502, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var generated, mainUnits, l1Units atomic.Int64
			var mainStatus atomic.Int32
			signedRequest := make(chan http.Header, 1)
			client := &http.Client{Timeout: 5 * time.Second}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				generated.Add(1)
				defer req.Body.Close()
				body, err := io.ReadAll(req.Body)
				if err != nil || req.Method != http.MethodPost || req.URL.Path != path || string(body) != requestBody ||
					imagerelay.HasRequest(req.Header) {
					t.Error("provider did not receive the intended public image request")
					http.Error(w, "unexpected request", 400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set(imagerelay.ResultHeader, "provider-header-attempt")
				_, _ = w.Write(providerBody)
			}))
			defer provider.Close()

			paired := config.ImageRelayConfig{
				Role: "main", SourceSite: "l1", DestinationSite: "main", MainAPIKeyID: mainKeyID,
				KeyID: "active", SecretHex: strings.Repeat("a1", 32), PrivateDir: filepath.Join(t.TempDir(), "claims"),
			}
			mainGateway := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{ImageRelay: paired}}}
			mainRuntime := mainGateway.imageRelayRuntime()
			if mainRuntime.err != nil {
				t.Fatal(mainRuntime.err)
			}
			t.Cleanup(func() {
				if err := errors.Join(mainRuntime.results.Close(), mainRuntime.journal.Close()); err != nil {
					t.Error(err)
				}
			})
			mainRouter := gin.New()
			mainRouter.POST(path, func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				if err != nil {
					c.Status(400)
					return
				}
				parsed, err := mainGateway.ParseOpenAIImagesRequest(c, body)
				if err != nil {
					c.Status(400)
					return
				}
				status, err := mainGateway.AuthorizeImageRelay(c, &APIKey{ID: mainKeyID, UserID: 1}, parsed, body)
				if err != nil {
					c.Status(status)
					return
				}
				select {
				case signedRequest <- c.Request.Header.Clone():
				default:
				}
				resp, err := client.Post(provider.URL+path, "application/json", bytes.NewReader(body))
				if err != nil {
					t.Error(err)
					c.Status(502)
					return
				}
				defer resp.Body.Close()
				usage, count, sizes, err := mainGateway.handleOpenAIImagesNonStreamingResponse(c.Request.Context(), resp, c, &Account{}, parsed)
				if err != nil || count != 1 || len(sizes) != 1 || sizes[0] != "2048x2048" || usage.OutputTokens != 20 {
					t.Errorf("main normalization/accounting inputs: count=%d sizes=%v usage=%+v err=%v", count, sizes, usage, err)
					return
				}
				mainUnits.Add(18)
			})
			mainRouter.POST(asyncPath, func(c *gin.Context) {
				// Workbench dispatch retains the registered route while the
				// synthetic image handler receives a rewritten URL path.
				c.Request.URL.Path = path
				parsed := &OpenAIImagesRequest{Model: "gpt-image-2", Endpoint: path}
				status, err := mainGateway.AuthorizeImageRelay(c, &APIKey{ID: mainKeyID}, parsed, []byte(requestBody))
				_, negotiated := inboundImageRelay(c.Request.Context())
				if negotiated || status != 0 || err != nil || mainGateway.ImageRelayDirectResponse(c, &Account{ID: accountID}, parsed) {
					t.Error("async ingress activated the sync image relay")
					c.Status(500)
					return
				}
				c.Status(204)
			})
			mainServer := httptest.NewServer(mainRouter)
			defer mainServer.Close()

			paired.Role = "l1"
			paired.L1AccountID = accountID
			paired.MainOrigin = mainServer.URL
			paired.PrivateDir = filepath.Join(t.TempDir(), "spool")
			l1Gateway := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{ImageRelay: paired}}}
			l1Gateway.imageRelayOnce.Do(func() {
				key, err := imagerelay.NewSigner("active", map[string][]byte{"active": bytes.Repeat([]byte{0xa1}, 32)})
				if err != nil {
					t.Fatal(err)
				}
				spool, err := imagerelay.NewSpool(paired.PrivateDir, 2, 256<<20, 0)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := spool.Close(); err != nil {
						t.Error(err)
					}
				})
				l1Gateway.imageRelay = &imageRelayRuntime{contract: paired, signer: key, spool: spool}
			})
			l1Router := gin.New()
			l1Router.POST(path, func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				if err != nil {
					c.Status(400)
					return
				}
				parsed, err := l1Gateway.ParseOpenAIImagesRequest(c, body)
				if err != nil {
					c.Status(400)
					return
				}
				req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, mainServer.URL+path, bytes.NewReader(body))
				if err != nil {
					t.Error(err)
					c.Status(500)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				exchange, err := l1Gateway.prepareImageRelayL1(c.Request.Context(), c, req, &Account{ID: accountID}, parsed, body)
				if err != nil || exchange == nil {
					t.Errorf("L1 negotiation did not start: %v", err)
					c.Status(500)
					return
				}
				defer exchange.close()
				if tc.editRequest != nil {
					tc.editRequest(req)
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Error(err)
					c.Status(502)
					return
				}
				defer resp.Body.Close()
				mainStatus.Store(int32(resp.StatusCode))
				if tc.editResponse != nil {
					tc.editResponse(resp)
				}
				usage, count, sizes, delivery, err := exchange.receive(c.Request.Context(), resp, parsed.Model)
				if err != nil {
					c.Status(502)
					return
				}
				defer delivery.close()
				if count != 1 || len(sizes) != 1 || sizes[0] != "2048x2048" || usage.OutputTokens != 20 {
					t.Errorf("L1 received unexpected usage: count=%d sizes=%v usage=%+v", count, sizes, usage)
					c.Status(500)
					return
				}
				l1Units.Add(25)
				if err := delivery.serve(c); err != nil {
					t.Error(err)
				}
			})
			l1Server := httptest.NewServer(l1Router)
			defer l1Server.Close()

			resp, err := client.Post(l1Server.URL+path, "application/json", strings.NewReader(requestBody))
			if err != nil {
				t.Fatal(err)
			}
			clientBody, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != tc.wantClientCode || int(mainStatus.Load()) != tc.wantMainCode {
				t.Fatalf("unexpected statuses: main=%d client=%d readErr=%v", mainStatus.Load(), resp.StatusCode, err)
			}
			if imagerelay.HasResult(resp.Header) {
				t.Fatal("private receipt leaked to the customer")
			}
			wantL1 := int64(0)
			if tc.wantClientCode == 200 {
				wantL1 = 25
				var payload struct {
					Size string `json:"size"`
				}
				err := json.Unmarshal(clientBody, &payload)
				if err != nil || payload.Size != "2048x2048" || !bytes.Equal(clientBody, normalized) {
					t.Fatal("L1 did not preserve the main gateway's normalized 2K response")
				}
				var captured http.Header
				select {
				case captured = <-signedRequest:
				default:
					t.Fatal("missing authorized internal request")
				}
				for _, attempt := range []struct {
					route string
					want  int
				}{{path, 409}, {asyncPath, 204}} {
					duplicate, err := http.NewRequestWithContext(context.Background(), http.MethodPost, mainServer.URL+attempt.route, strings.NewReader(requestBody))
					if err != nil {
						t.Fatal(err)
					}
					duplicate.Header = captured.Clone()
					reply, err := client.Do(duplicate)
					if err != nil {
						t.Fatal(err)
					}
					reply.Body.Close()
					if reply.StatusCode != attempt.want || imagerelay.HasResult(reply.Header) {
						t.Fatalf("same-operation request to %s returned %d", attempt.route, reply.StatusCode)
					}
				}
			}
			if generated.Load() != tc.wantProviderCalls || mainUnits.Load() != tc.wantProviderCalls*18 || l1Units.Load() != wantL1 {
				t.Fatalf("fixture ledger differs from expected single dispatch: provider=%d main=%d L1=%d", generated.Load(), mainUnits.Load(), l1Units.Load())
			}
		})
	}
}

type prefixedImageRelayBody struct {
	io.ReadCloser
	reader io.Reader
}

func (b prefixedImageRelayBody) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}
