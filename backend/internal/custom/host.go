package custom

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// Host is the application-facing adapter for the custom provider set.
//
// Keeping this adapter in the custom package gives the upstream server a
// narrow, stable boundary: it only needs to know how to hand over an admin
// router, a gateway router, and the two provider registrars. Feature packages
// do not need to import server or wire packages, and an empty Host is a true
// no-op.
type Host struct {
	providers *CustomProviderSet
}

// NewHost creates a host adapter. A nil provider set is normalized to an
// empty set so callers can safely use the returned host without nil checks.
func NewHost(providers *CustomProviderSet) *Host {
	if providers == nil {
		providers = &CustomProviderSet{}
	}
	return &Host{providers: providers}
}

// Providers returns the provider set owned by this host. The set itself is
// immutable after construction by NewCustomProviderSet; the returned pointer
// is intended for inspection and registration only.
func (h *Host) Providers() *CustomProviderSet {
	if h == nil {
		return nil
	}
	return h.providers
}

// RegisterAdminRoutes mounts every custom admin route provider in deterministic
// provider-ID order. Empty hosts do nothing.
func (h *Host) RegisterAdminRoutes(router gin.IRouter) {
	if h == nil || router == nil {
		return
	}
	for _, provider := range h.providers.Routes() {
		provider.RegisterAdminRoutes(router)
	}
}

// RegisterGatewayRoutes mounts every custom gateway route provider in
// deterministic provider-ID order. Empty hosts do nothing.
func (h *Host) RegisterGatewayRoutes(router gin.IRouter) {
	if h == nil || router == nil {
		return
	}
	for _, provider := range h.providers.Routes() {
		provider.RegisterGatewayRoutes(router)
	}
}

// PermissionRegistrar is implemented by the authorization host. It is kept
// deliberately small so custom permission providers do not depend on the
// core role or settings implementation.
type PermissionRegistrar interface {
	RegisterPermissionProvider(PermissionProvider) error
}

// SettingRegistrar is implemented by the settings host. The registrar owns
// validation, persistence, and conflict handling for the provider's settings.
type SettingRegistrar interface {
	RegisterSettingProvider(SettingProvider) error
}

// RegisterPermissions attaches all custom permission providers to a host.
// The empty set is a no-op, while a non-empty set requires a registrar.
func (h *Host) RegisterPermissions(registrar PermissionRegistrar) error {
	providers := h.permissionProviders()
	if len(providers) == 0 {
		return nil
	}
	if registrar == nil {
		return ErrNilRegistrar
	}
	for _, provider := range providers {
		if err := registrar.RegisterPermissionProvider(provider); err != nil {
			return fmt.Errorf("register custom permission provider %q: %w", provider.ID(), err)
		}
	}
	return nil
}

// RegisterSettings attaches all custom setting providers to a host.
// The empty set is a no-op, while a non-empty set requires a registrar.
func (h *Host) RegisterSettings(registrar SettingRegistrar) error {
	providers := h.settingProviders()
	if len(providers) == 0 {
		return nil
	}
	if registrar == nil {
		return ErrNilRegistrar
	}
	for _, provider := range providers {
		if err := registrar.RegisterSettingProvider(provider); err != nil {
			return fmt.Errorf("register custom setting provider %q: %w", provider.ID(), err)
		}
	}
	return nil
}

// StartWorkers starts each custom worker in deterministic provider-ID order.
// Workers are only started when the application explicitly invokes this
// method; constructing a Host has no side effects. If multiple workers fail,
// all errors are returned so startup diagnostics do not hide later failures.
func (h *Host) StartWorkers(ctx context.Context) error {
	if h == nil {
		return nil
	}
	var errs []error
	for _, worker := range h.providers.Workers() {
		if err := worker.Start(ctx); err != nil {
			errs = append(errs, fmt.Errorf("start custom worker %q: %w", worker.ID(), err))
		}
	}
	return errors.Join(errs...)
}

func (h *Host) permissionProviders() []PermissionProvider {
	if h == nil || h.providers == nil {
		return nil
	}
	return h.providers.Permissions()
}

func (h *Host) settingProviders() []SettingProvider {
	if h == nil || h.providers == nil {
		return nil
	}
	return h.providers.Settings()
}

var (
	// ErrNilRegistrar indicates that providers exist but the application did
	// not supply the host responsible for registering them.
	ErrNilRegistrar = errors.New("nil custom provider registrar")

	defaultHost atomic.Pointer[Host]
)

func init() {
	defaultHost.Store(NewHost(nil))
}

// DefaultHost returns the process-wide host used by route registration. The
// default is empty, preserving the upstream behavior until a custom build
// configures its providers before starting the HTTP server.
func DefaultHost() *Host {
	host := defaultHost.Load()
	if host == nil {
		return NewHost(nil)
	}
	return host
}

// Configure replaces the process-wide host after validating the supplied
// providers. It is intended to be called once during application bootstrap,
// before route registration and worker startup.
func Configure(hooks CustomHooks) error {
	providers, err := NewCustomProviderSet(hooks)
	if err != nil {
		return err
	}
	defaultHost.Store(NewHost(providers))
	return nil
}

// ConfigureProviderSet installs an already validated provider set. A nil set
// resets the process-wide host to an empty host, which is useful for tests and
// for builds that disable all custom features.
func ConfigureProviderSet(providers *CustomProviderSet) {
	defaultHost.Store(NewHost(providers))
}
