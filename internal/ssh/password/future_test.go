package password

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/secret"
)

func TestFutureResolveWaitsForInFlightLookupAndRunsOnce(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32

	future := NewFuture(func(ctx context.Context) (*secret.Secret, error) {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}
		return secret.NewFromString("secret"), nil
	})
	defer future.Close()

	future.Start(context.Background())
	<-started

	resolved := make(chan *secret.Secret, 2)
	for range 2 {
		go func() {
			pw, err := future.Resolve(context.Background())
			if err != nil {
				t.Errorf("Resolve: %v", err)
				return
			}
			resolved <- pw
		}()
	}

	select {
	case <-resolved:
		t.Fatal("Resolve returned before in-flight lookup completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	first := <-resolved
	second := <-resolved
	if first == nil || second == nil || first != second {
		t.Fatalf("Resolve returned inconsistent secrets: %p %p", first, second)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
}

func TestFutureResolveStartsLazyLookupWhenNotPrefetched(t *testing.T) {
	var calls atomic.Int32
	future := NewFuture(func(ctx context.Context) (*secret.Secret, error) {
		calls.Add(1)
		return secret.NewFromString("secret"), nil
	})
	defer future.Close()

	pw, err := future.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if pw == nil {
		t.Fatal("Resolve returned nil secret")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
}
