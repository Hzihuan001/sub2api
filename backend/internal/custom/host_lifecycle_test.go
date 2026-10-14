package custom

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lifecycleWorker struct {
	id       string
	start    func(context.Context) error
	shutdown func(context.Context) error
}

func (w lifecycleWorker) ID() string                         { return w.id }
func (w lifecycleWorker) Start(ctx context.Context) error    { return w.start(ctx) }
func (w lifecycleWorker) Shutdown(ctx context.Context) error { return w.shutdown(ctx) }

func TestHostWorkerLifecycleStartsOnceAndShutsDownInReverseOrder(t *testing.T) {
	var calls []string
	newWorker := func(id string) lifecycleWorker {
		return lifecycleWorker{id, func(context.Context) error { calls = append(calls, "start:"+id); return nil },
			func(context.Context) error { calls = append(calls, "stop:"+id); return nil }}
	}
	set, err := NewCustomProviderSet(testHooks{workers: []WorkerProvider{newWorker("z"), newWorker("a")}})
	require.NoError(t, err)
	h := NewHost(set)
	require.Empty(t, calls)
	require.NoError(t, h.StartWorkers(context.Background()))
	require.NoError(t, h.StartWorkers(context.Background()))
	require.NoError(t, h.ShutdownWorkers(context.Background()))
	require.NoError(t, h.ShutdownWorkers(context.Background()))
	require.Equal(t, []string{"start:a", "start:z", "stop:z", "stop:a"}, calls)
}

func TestHostWorkerStartFailureCancelsAndWaitsForPartialStarts(t *testing.T) {
	startErr := errors.New("start failed after acquiring resources")
	var stopped atomic.Int32
	var done [2]chan struct{}
	var order []int
	workers := make([]WorkerProvider, 0, 2)
	for i := 0; i < 2; i++ {
		index := i
		done[index] = make(chan struct{})
		workers = append(workers, lifecycleWorker{
			id: []string{"a", "b"}[index],
			start: func(ctx context.Context) error {
				go func() { <-ctx.Done(); stopped.Add(1); close(done[index]) }()
				if index == 0 {
					return startErr
				}
				return nil
			},
			shutdown: func(ctx context.Context) error {
				order = append(order, index)
				select {
				case <-done[index]:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		})
	}
	set, err := NewCustomProviderSet(testHooks{workers: workers})
	require.NoError(t, err)
	h := NewHost(set)
	require.ErrorIs(t, h.StartWorkers(context.Background()), startErr)
	require.EqualValues(t, 2, stopped.Load(), "failed startup must not leave either partial or successful starts running")
	require.Equal(t, []int{1, 0}, order)
	require.NoError(t, h.ShutdownWorkers(context.Background()))
	require.Len(t, order, 2)
}

func TestHostWorkerShutdownTimeoutPreventsOverlappingRestart(t *testing.T) {
	var starts atomic.Int32
	release := make(chan struct{})
	w := lifecycleWorker{
		id:    "slow-worker",
		start: func(context.Context) error { starts.Add(1); return nil },
		shutdown: func(ctx context.Context) error {
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	set, err := NewCustomProviderSet(testHooks{workers: []WorkerProvider{w}})
	require.NoError(t, err)
	h := NewHost(set)
	require.NoError(t, h.StartWorkers(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	require.ErrorIs(t, h.ShutdownWorkers(ctx), context.DeadlineExceeded)
	require.Error(t, h.StartWorkers(context.Background()))
	require.EqualValues(t, 1, starts.Load())
	close(release)
	require.NoError(t, h.ShutdownWorkers(context.Background()))
	require.NoError(t, h.StartWorkers(context.Background()))
	require.EqualValues(t, 2, starts.Load())
	require.NoError(t, h.ShutdownWorkers(context.Background()))
}

func TestHostWorkerCanceledStartDoesNotInvokeProvider(t *testing.T) {
	started := false
	set, err := NewCustomProviderSet(testHooks{workers: []WorkerProvider{lifecycleWorker{
		id: "worker", start: func(context.Context) error { started = true; return nil }, shutdown: func(context.Context) error { return nil },
	}}})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := NewHost(set)
	require.ErrorIs(t, h.StartWorkers(ctx), context.Canceled)
	require.False(t, started)
	require.NoError(t, h.ShutdownWorkers(context.Background()))
}
