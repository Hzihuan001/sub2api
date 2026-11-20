package handler

import (
	"context"
	"sync"
	"time"
)

type imageConcurrencyLimiter struct {
	mu      sync.Mutex
	notify  chan struct{}
	limit   int
	active  int
	waiting int
	enabled bool
}

type imageAdmission uint8

const (
	imageAdmitted imageAdmission = iota
	imageAtCapacity
	imageQueueFull
	imageQueueTimeout
	imageRequestEnded
)

func (l *imageConcurrencyLimiter) TryAcquire(enabled bool, limit int) (func(), bool) {
	return l.Acquire(context.Background(), enabled, limit, false, 0, 0)
}

func (l *imageConcurrencyLimiter) Acquire(ctx context.Context, enabled bool, limit int, wait bool, timeout time.Duration, maxWaiting int) (func(), bool) {
	release, admission := l.acquire(ctx, enabled, limit, wait, timeout, maxWaiting)
	return release, admission == imageAdmitted
}

func (l *imageConcurrencyLimiter) acquire(ctx context.Context, enabled bool, limit int, wait bool, timeout time.Duration, maxWaiting int) (func(), imageAdmission) {
	if ctx == nil {
		ctx = context.Background()
	}
	parent := ctx
	if parent.Err() != nil {
		return nil, imageRequestEnded
	}
	if !enabled || limit <= 0 {
		return nil, imageAdmitted
	}
	if wait {
		if timeout <= 0 {
			return nil, imageQueueTimeout
		}
		waitCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		ctx = waitCtx
	}
	if maxWaiting < 0 {
		maxWaiting = 0
	}
	stopped := func() imageAdmission {
		if parent.Err() != nil {
			return imageRequestEnded
		}
		return imageQueueTimeout
	}
	for {
		if ctx.Err() != nil {
			return nil, stopped()
		}
		release, acquired, waitRelease, notify := l.tryAcquireLocked(enabled, limit, wait, maxWaiting)
		if acquired {
			if ctx.Err() != nil {
				release()
				return nil, stopped()
			}
			return release, imageAdmitted
		}
		if !wait {
			return nil, imageAtCapacity
		}
		if notify == nil {
			if ctx.Err() != nil {
				return nil, stopped()
			}
			return nil, imageQueueFull
		}
		if !l.waitForSlot(ctx, notify) {
			if waitRelease != nil {
				waitRelease()
			}
			return nil, stopped()
		}
		if waitRelease != nil {
			waitRelease()
		}
	}
}

func (l *imageConcurrencyLimiter) tryAcquireLocked(enabled bool, limit int, wait bool, maxWaiting int) (func(), bool, func(), <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.notify == nil {
		l.notify = make(chan struct{})
	}
	if l.enabled != enabled || l.limit != limit {
		l.enabled = enabled
		l.limit = limit
	}
	if l.active < l.limit {
		l.active++
		return l.releaseFunc(), true, nil, nil
	}
	if !wait {
		return nil, false, nil, nil
	}
	if maxWaiting > 0 && l.waiting >= maxWaiting {
		return nil, false, nil, nil
	}
	l.waiting++
	return nil, false, l.waiterReleaseFunc(), l.notify
}

func (l *imageConcurrencyLimiter) waitForSlot(ctx context.Context, notify <-chan struct{}) bool {
	select {
	case <-notify:
		return true
	case <-ctx.Done():
		return false
	}
}

func (l *imageConcurrencyLimiter) releaseFunc() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			if l.active > 0 {
				l.active--
			}
			if l.notify != nil {
				close(l.notify)
				l.notify = make(chan struct{})
			}
			l.mu.Unlock()
		})
	}
}

func (l *imageConcurrencyLimiter) waiterReleaseFunc() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			if l.waiting > 0 {
				l.waiting--
			}
			l.mu.Unlock()
		})
	}
}
