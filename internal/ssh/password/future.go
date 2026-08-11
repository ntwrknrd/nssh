package password

import (
	"context"
	"fmt"
	"sync"

	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

type Resolver func(context.Context) (*secret.Secret, error)

type Future struct {
	resolver Resolver

	mu      sync.Mutex
	started bool
	done    chan struct{}
	cancel  context.CancelFunc
	pw      *secret.Secret
	err     error
	closed  bool
}

func NewFuture(resolver Resolver) *Future {
	return &Future{resolver: resolver}
}

func (f *Future) Start(ctx context.Context) {
	if f == nil {
		return
	}
	f.start(ctx)
}

func (f *Future) Resolve(ctx context.Context) (*secret.Secret, error) {
	if f == nil {
		return nil, nil
	}
	done := f.start(ctx)
	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pw, f.err
}

func (f *Future) Close() {
	if f == nil {
		return
	}
	f.mu.Lock()
	cancel := f.cancel
	pw := f.pw
	f.pw = nil
	f.closed = true
	f.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if pw != nil {
		pw.Destroy()
	}
}

func (f *Future) start(ctx context.Context) <-chan struct{} {
	f.mu.Lock()
	if f.started {
		done := f.done
		f.mu.Unlock()
		return done
	}
	if f.resolver == nil {
		f.started = true
		f.done = closedDone()
		f.err = fmt.Errorf("password resolver is required")
		done := f.done
		f.mu.Unlock()
		return done
	}
	f.started = true
	f.done = make(chan struct{})
	resolveCtx, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	done := f.done
	resolver := f.resolver
	f.mu.Unlock()

	go func() {
		timer := connector.StartTiming(connector.TimingCredentialPrefetch)
		pw, err := resolver(resolveCtx)
		timer.Emit()
		f.mu.Lock()
		if f.closed {
			f.mu.Unlock()
			if pw != nil {
				pw.Destroy()
			}
			close(done)
			return
		}
		f.pw = pw
		f.err = err
		f.mu.Unlock()
		close(done)
	}()
	return done
}

func closedDone() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}
