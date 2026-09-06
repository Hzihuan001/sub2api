package moshureseller

import (
	"context"
	"sync"
	"time"
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
	go runtime.run(ctx, service)
	return runtime
}

func (r *Runtime) run(ctx context.Context, service *Service) {
	defer close(r.done)
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
