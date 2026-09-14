package credential

import (
	"context"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/providerexec"
)

// NewCommandRegistry binds provider requests to command cancellation. Command
// clients use existing provider sessions; they never take over terminal input
// to bootstrap a new session.
func NewCommandRegistry(ctx context.Context, cfg *config.Config) (*Registry, error) {
	r, err := NewRegistry(cfg)
	if err != nil {
		return nil, err
	}
	for _, provider := range r.providers {
		switch p := provider.(type) {
		case *bitwardenProvider:
			p.noUnlock = true
			p.transport = commandTransport(ctx, p.transport)
		case *onePasswordProvider:
			p.transport = commandTransport(ctx, p.transport)
		case *sopsAgeProvider:
			p.transport = commandTransport(ctx, p.transport)
		}
	}
	return r, nil
}

type commandProviderTransport struct{ inner providerTransport }

func commandTransport(ctx context.Context, transport providerTransport) providerTransport {
	switch t := transport.(type) {
	case directProviderTransport:
		t.ctx = ctx
		transport = t
	case agentProviderTransport:
		t.ctx = ctx
		transport = t
	}
	return commandProviderTransport{inner: transport}
}
func (t commandProviderTransport) ProviderRequest(req providerexec.ProviderRequest) (*providerexec.ProviderResponse, error) {
	req.NonInteractive = true
	if req.Action == "get" {
		req.Action = "get_noninteractive"
	}
	// A distinct action makes an older agent fail closed instead of silently
	// ignoring the new interaction policy field.
	return t.inner.ProviderRequest(req)
}
