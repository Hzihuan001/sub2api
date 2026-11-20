package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/imagerelay"
	"github.com/gin-gonic/gin"
)

const imageRelayFixtureModel = "gpt-image-2"

func TestImageRelayGatewayFailureDoesNotRequestRepost(t *testing.T) {
	gateway := &OpenAIGatewayService{}
	account := &Account{Type: AccountTypeAPIKey, Platform: PlatformOpenAI}
	for _, allowFailover := range []bool{false, true} {
		writer := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(writer)
		c.Request = httptest.NewRequest(http.MethodPost, openAIImagesGenerationsEndpoint, nil)
		response := &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"type":"upstream_error","message":"Upstream request failed"}}`)),
		}
		_, err := gateway.handleOpenAIImagesHTTPError(c.Request.Context(), response, c, account, allowFailover, imageRelayFixtureModel)
		if allowFailover {
			var failure *UpstreamFailoverError
			if !errors.As(err, &failure) || !failure.RetryableOnSameAccount || c.Writer.Written() {
				t.Fatalf("legacy image failure did not retain its retry policy: %v", err)
			}
			continue
		}
		var failure *OpenAIImagesUpstreamError
		if !errors.As(err, &failure) || writer.Code != http.StatusBadGateway || !c.Writer.Written() {
			t.Fatalf("relay did not finish on the first gateway failure: %v, status %d", err, writer.Code)
		}
	}
}

func imageRelayL1Fixture(t *testing.T, sent []byte) (*imageRelayOutbound, *http.Response, *imagerelay.Spool) {
	t.Helper()
	signer, err := imagerelay.NewSigner("active", map[string][]byte{"active": bytes.Repeat([]byte{0x83}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	spool, err := imagerelay.NewSpool(filepath.Join(t.TempDir(), "spool"), 1, 1024, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := spool.Close(); err != nil {
			t.Error(err)
		}
	})
	lease, err := spool.Reserve(1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lease.Close() })
	request, err := signer.SignRequest(make(http.Header), imagerelay.Binding{
		Source:       "reseller",
		Destination:  "main",
		OperationID:  "d20ec3c5fdbf4d69ad4f8e300bc16c30",
		MainAPIKeyID: 7,
		Method:       http.MethodPost,
		Path:         "/v1/images/generations",
		ContentType:  "application/json",
		BodySHA256:   imagerelay.SHA256Hex([]byte("image prompt")),
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	outbound := &imageRelayOutbound{signer: signer, binding: request, reservation: lease}
	response := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(sent)),
		Body:          io.NopCloser(bytes.NewReader(sent)),
		Header:        http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"fixture-id"}},
	}
	err = signer.SignResult(response.Header, request, imagerelay.Metadata{
		BodyBytes: int64(len(sent)), BodySHA256: imagerelay.SHA256Hex(sent),
		ImageCount: 1, ImageSizes: []string{"2048x2048"}, Model: imageRelayFixtureModel,
		Usage: imagerelay.Usage{ImageOutputTokens: 21, CacheCreationInputTokensPresent: true},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return outbound, response, spool
}

func TestImageRelayL1DeliveryUsesVerifiedBodyAndPublicHeaders(t *testing.T) {
	input := []byte("{\"data\":[{\"b64_json\":\"pasteful-payload\"}]}")
	outbound, response, spool := imageRelayL1Fixture(t, input)
	usage, count, sizes, result, err := outbound.receive(context.Background(), response, imageRelayFixtureModel)
	if err != nil {
		t.Fatal(err)
	}
	defer result.close()
	if imagerelay.HasResult(response.Header) {
		t.Fatal("private receipt remained available to the gateway header forwarding code")
	}
	if usage.ImageOutputTokens != 21 || !usage.cacheCreationInputTokensPresent || count != 1 ||
		len(sizes) != 1 || sizes[0] != "2048x2048" {
		t.Fatalf("wrong observed image metadata: %+v, %d, %v", usage, count, sizes)
	}
	if _, err := spool.Reserve(1); !errors.Is(err, imagerelay.ErrSpoolCapacity) {
		t.Fatalf("disk reservation released before public delivery finished: %v", err)
	}
	router := gin.New()
	router.POST("/v1/images/generations", func(c *gin.Context) {
		if err := result.serve(c); err != nil {
			t.Error(err)
		}
	})
	responseRecorder := httptest.NewRecorder()
	router.ServeHTTP(responseRecorder, httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil))
	if responseRecorder.Code != 200 || !bytes.Equal(input, responseRecorder.Body.Bytes()) ||
		responseRecorder.Header().Get("X-Request-Id") != "fixture-id" || imagerelay.HasResult(responseRecorder.Header()) {
		t.Fatalf("incompatible client response: status %d", responseRecorder.Code)
	}
}

func TestImageRelayL1RejectsIncompatibleOrTruncatedSuccess(t *testing.T) {
	for _, scenario := range []struct {
		name string
		edit func(*http.Response)
	}{
		{"missing receipt", func(r *http.Response) { r.Header.Del(imagerelay.ResultHeader) }},
		{"unexpected content length", func(r *http.Response) { r.ContentLength++ }},
		{"unexpected transfer encoding", func(r *http.Response) { r.TransferEncoding = []string{"chunked"} }},
		{"unframed content", func(r *http.Response) { r.ContentLength = -1 }},
		{"unexpected content encoding", func(r *http.Response) { r.Header.Set("Content-Encoding", "gzip") }},
		{"truncated", func(r *http.Response) { r.Body = io.NopCloser(bytes.NewReader([]byte{0x7b})) }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			outbound, response, _ := imageRelayL1Fixture(t, []byte("{}"))
			scenario.edit(response)
			_, _, _, delivery, err := outbound.receive(context.Background(), response, imageRelayFixtureModel)
			if delivery != nil || !errors.Is(err, errImageRelayResponse) {
				t.Fatalf("invalid result available for charging and delivery: %v, %v", delivery, err)
			}
			if imagerelay.HasResult(response.Header) {
				t.Fatal("internal header retained after validation failure")
			}
		})
	}
}

func TestImageRelayL1OriginStrictlyMatchesConfiguredMainSite(t *testing.T) {
	const origin = "http://image-main.test:8080"
	for _, scenario := range []struct {
		target string
		host   string
		match  bool
	}{
		{origin + "/v1/images/edits", "", true},
		{origin + "/v1/images/edits", "other.test", false},
		{origin + "/v1/images/edits?forbidden=1", "", false},
		{"https://image-main.test:8080/v1/images/edits", "", false},
		{"http://image-main.test/v1/images/edits", "", false},
		{"http://image-main.test:8080@another.test/v1/images/edits", "", false},
	} {
		request := httptest.NewRequest(http.MethodPost, scenario.target, nil)
		if scenario.host != "" {
			request.Host = scenario.host
		}
		if actual := imageRelayMatchesMainOrigin(request, origin); actual != scenario.match {
			t.Fatalf("unexpected origin match for target %q and Host %q", scenario.target, request.Host)
		}
	}
}

func TestImageRelayL1SignsOnlyPairedRequestAfterRewrites(t *testing.T) {
	const origin = "http://main.internal:8080"
	const path = "/v1/images/generations"
	body := []byte("{\"model\":\"gpt-image-2\"}")
	signer, err := imagerelay.NewSigner("release", map[string][]byte{"release": bytes.Repeat([]byte{0x81}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	spool, err := imagerelay.NewSpool(filepath.Join(t.TempDir(), "spool"), 1, 1024, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	gateway := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		UpstreamResponseReadMaxBytes: 1024,
		ImageRelay: config.ImageRelayConfig{
			Role: "l1", SourceSite: "l1", DestinationSite: "main", L1AccountID: 11, MainAPIKeyID: 22, MainOrigin: origin,
		},
	}}}
	gateway.imageRelayOnce.Do(func() {
		gateway.imageRelay = &imageRelayRuntime{contract: gateway.cfg.Gateway.ImageRelay, signer: signer, spool: spool}
	})
	router := gin.New()
	router.POST(path, func(c *gin.Context) {
		for _, scenario := range []struct {
			account    int64
			target     string
			wantSigned bool
			wantError  bool
		}{
			{9, origin + path, false, false},
			{11, "http://unexpected.internal:8080" + path, false, true},
			{11, origin + path, true, false},
		} {
			request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, scenario.target, bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(imagerelay.RequestHeader, "untrusted override")
			outbound, err := gateway.prepareImageRelayL1(c.Request.Context(), c, request, &Account{ID: scenario.account},
				&OpenAIImagesRequest{Endpoint: path}, body)
			if (err != nil) != scenario.wantError || (outbound != nil) != scenario.wantSigned ||
				imagerelay.HasRequest(request.Header) != scenario.wantSigned {
				t.Fatalf("unexpected relay selection for account %d: %v", scenario.account, err)
			}
			if outbound == nil {
				continue
			}
			defer outbound.close()
			if request.GetBody != nil || !HTTPUpstreamRedirectsDisabled(request.Context()) ||
				request.Header.Get("Accept-Encoding") != "identity" {
				t.Fatal("signed requests must not follow redirects, replay through GetBody, or negotiate compressed results")
			}
			requestBinding, err := signer.VerifyRequest(request.Header, imagerelay.Binding{
				Source: "l1", Destination: "main", MainAPIKeyID: 22, Method: http.MethodPost, Path: path,
				ContentType: "application/json", BodySHA256: imagerelay.SHA256Hex(body),
			}, time.Now())
			if err != nil || requestBinding != outbound.binding {
				t.Fatalf("request body rewrites were not bound to the signed negotiation: %v", err)
			}
			if _, err := spool.Reserve(1); !errors.Is(err, imagerelay.ErrSpoolCapacity) {
				t.Fatalf("request signed without reserving capacity: %v", err)
			}
		}
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
}
