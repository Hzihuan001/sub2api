package custom

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type hostRouteProvider struct {
	id      string
	admin   *[]string
	gateway *[]string
}

func (p hostRouteProvider) ID() string { return p.id }

func (p hostRouteProvider) RegisterAdminRoutes(router gin.IRouter) {
	*p.admin = append(*p.admin, p.id)
	router.GET("/custom-"+p.id, func(c *gin.Context) { c.Status(http.StatusNoContent) })
}

func (p hostRouteProvider) RegisterGatewayRoutes(router gin.IRouter) {
	*p.gateway = append(*p.gateway, p.id)
	router.GET("/custom-"+p.id, func(c *gin.Context) { c.Status(http.StatusNoContent) })
}

type hostPermissionRegistrar struct{ ids []string }

func (r *hostPermissionRegistrar) RegisterPermissionProvider(p PermissionProvider) error {
	r.ids = append(r.ids, p.ID())
	return nil
}

type hostSettingRegistrar struct{ ids []string }

func (r *hostSettingRegistrar) RegisterSettingProvider(p SettingProvider) error {
	r.ids = append(r.ids, p.ID())
	return nil
}

type hostWorker struct {
	id    string
	start *[]string
	err   error
}

func (w hostWorker) ID() string { return w.id }

func (w hostWorker) Start(context.Context) error {
	*w.start = append(*w.start, w.id)
	return w.err
}

func TestHostRegistersRoutesAndProviderCategories(t *testing.T) {
	adminIDs := []string{}
	gatewayIDs := []string{}
	set, err := NewCustomProviderSet(testHooks{
		routes: []RouteProvider{
			hostRouteProvider{id: "z-feature", admin: &adminIDs, gateway: &gatewayIDs},
			hostRouteProvider{id: "a-feature", admin: &adminIDs, gateway: &gatewayIDs},
		},
		permissions: []PermissionProvider{
			testPermissionProvider{id: "z-permission"},
			testPermissionProvider{id: "a-permission"},
		},
		settings: []SettingProvider{
			testSettingProvider{id: "z-setting"},
			testSettingProvider{id: "a-setting"},
		},
	})
	if err != nil {
		t.Fatalf("NewCustomProviderSet() error = %v", err)
	}

	host := NewHost(set)
	adminRouter := gin.New()
	gatewayRouter := gin.New()
	host.RegisterAdminRoutes(adminRouter)
	host.RegisterGatewayRoutes(gatewayRouter)
	if !reflect.DeepEqual(adminIDs, []string{"a-feature", "z-feature"}) {
		t.Fatalf("admin provider order = %v", adminIDs)
	}
	if !reflect.DeepEqual(gatewayIDs, []string{"a-feature", "z-feature"}) {
		t.Fatalf("gateway provider order = %v", gatewayIDs)
	}

	for _, router := range []*gin.Engine{adminRouter, gatewayRouter} {
		req := httptest.NewRequest(http.MethodGet, "/custom-a-feature", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("custom route status = %d, want %d", rec.Code, http.StatusNoContent)
		}
	}

	permissions := &hostPermissionRegistrar{}
	if err := host.RegisterPermissions(permissions); err != nil {
		t.Fatalf("RegisterPermissions() error = %v", err)
	}
	if !reflect.DeepEqual(permissions.ids, []string{"a-permission", "z-permission"}) {
		t.Fatalf("permission provider order = %v", permissions.ids)
	}
	settings := &hostSettingRegistrar{}
	if err := host.RegisterSettings(settings); err != nil {
		t.Fatalf("RegisterSettings() error = %v", err)
	}
	if !reflect.DeepEqual(settings.ids, []string{"a-setting", "z-setting"}) {
		t.Fatalf("setting provider order = %v", settings.ids)
	}
}

func TestHostEmptySetIsNoOp(t *testing.T) {
	host := NewHost(nil)
	host.RegisterAdminRoutes(gin.New())
	host.RegisterGatewayRoutes(gin.New())
	if err := host.RegisterPermissions(nil); err != nil {
		t.Fatalf("empty RegisterPermissions() error = %v", err)
	}
	if err := host.RegisterSettings(nil); err != nil {
		t.Fatalf("empty RegisterSettings() error = %v", err)
	}
	if err := host.StartWorkers(context.Background()); err != nil {
		t.Fatalf("empty StartWorkers() error = %v", err)
	}
}

func TestHostRegistrarsRequireRegistrarOnlyWhenNeeded(t *testing.T) {
	set, err := NewCustomProviderSet(testHooks{
		permissions: []PermissionProvider{testPermissionProvider{id: "permission"}},
		settings:    []SettingProvider{testSettingProvider{id: "setting"}},
	})
	if err != nil {
		t.Fatalf("NewCustomProviderSet() error = %v", err)
	}
	host := NewHost(set)
	if !errors.Is(host.RegisterPermissions(nil), ErrNilRegistrar) {
		t.Fatal("RegisterPermissions(nil) did not return ErrNilRegistrar")
	}
	if !errors.Is(host.RegisterSettings(nil), ErrNilRegistrar) {
		t.Fatal("RegisterSettings(nil) did not return ErrNilRegistrar")
	}
}

func TestHostStartsAllWorkersAndJoinsErrors(t *testing.T) {
	started := []string{}
	set, err := NewCustomProviderSet(testHooks{
		workers: []WorkerProvider{
			hostWorker{id: "z-worker", start: &started, err: errors.New("z failed")},
			hostWorker{id: "a-worker", start: &started, err: errors.New("a failed")},
		},
	})
	if err != nil {
		t.Fatalf("NewCustomProviderSet() error = %v", err)
	}
	err = NewHost(set).StartWorkers(context.Background())
	if err == nil {
		t.Fatal("StartWorkers() returned nil error")
	}
	if !reflect.DeepEqual(started, []string{"a-worker", "z-worker"}) {
		t.Fatalf("worker start order = %v", started)
	}
	if got := err.Error(); got == "" || !containsAll(got, "a failed", "z failed") {
		t.Fatalf("joined worker error = %q", got)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
