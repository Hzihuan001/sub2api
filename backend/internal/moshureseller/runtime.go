package moshureseller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	core "github.com/Wei-Shaw/sub2api/internal/service"
)

type Runtime struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func NewRuntime(service *Service) *Runtime {
	runtime := &Runtime{done: make(chan struct{})}
	if !Enabled() {
		close(runtime.done)
		return runtime
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime.cancel = cancel
	service.pricingEnabled = true
	core.SuspendResellerPricing()
	loadCtx, loadCancel := context.WithTimeout(ctx, 10*time.Second)
	service.configMu.Lock()
	if err := service.restorePricingLocked(loadCtx); err != nil {
		slog.Error("failed to restore reseller pricing", "error", err)
	}
	service.configMu.Unlock()
	loadCancel()
	// First deployment has no local cache. Warm it before the HTTP server
	// starts; subsequent restarts use the persisted generation without network.
	if core.ResellerPricingNeedsSync() {
		warmCtx, warmCancel := context.WithTimeout(ctx, 30*time.Second)
		if err := service.SyncPricing(warmCtx); err != nil {
			slog.Warn("initial reseller pricing sync pending", "error", err)
		}
		warmCancel()
	}
	go runtime.run(ctx, service)
	return runtime
}

func (r *Runtime) run(ctx context.Context, service *Service) {
	defer close(r.done)
	pricingDone := make(chan struct{})
	go func() { defer close(pricingDone); service.runPricingSync(ctx) }()
	defer func() { <-pricingDone }()
	settlementTicker := time.NewTicker(time.Minute)
	catalogTicker := time.NewTicker(5 * time.Minute)
	defer settlementTicker.Stop()
	defer catalogTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-settlementTicker.C:
			runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			_, _ = service.SyncSettlements(runCtx)
			cancel()
		case <-catalogTicker.C:
			runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			_, _ = service.SyncCatalog(runCtx)
			cancel()
		}
	}
}

func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		<-r.done
	})
}
