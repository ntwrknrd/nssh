package credential

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/providerexec"
)

type canceledLookupExecutor struct{ request providerexec.ProviderRequest }

func (e *canceledLookupExecutor) HandleProviderRequest(ctx context.Context, req providerexec.ProviderRequest) (providerexec.ProviderResponse, error) {
	e.request = req
	<-ctx.Done()
	return providerexec.ProviderResponse{}, ctx.Err()
}

func TestContextLookupRebindsCommandTransportWithoutMutatingProvider(t *testing.T) {
	executor := &canceledLookupExecutor{}
	// This also covers providers already bound to a REPL command context.
	base := commandTransport(context.Background(), directProviderTransport{executor: executor, timeout: time.Minute})
	provider := &sopsAgeProvider{name: "sops", transport: base}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := GetRefContext(ctx, provider, config.CredentialRefConfig{Ref: "test.password"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if provider.transport != base {
		t.Fatal("mutated shared provider")
	}
	if !executor.request.NonInteractive {
		t.Fatal("automatic lookup could open sign-in prompt")
	}
}
