package credential

import (
	"context"
	"errors"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/providerexec"
)

type commandTestTransport struct {
	req providerexec.ProviderRequest
	err error
}

func (t *commandTestTransport) ProviderRequest(req providerexec.ProviderRequest) (*providerexec.ProviderResponse, error) {
	t.req = req
	return nil, t.err
}

func TestCommandBitwardenDoesNotUnlock(t *testing.T) {
	old := unlockBitwardenProvider
	t.Cleanup(func() { unlockBitwardenProvider = old })
	unlockBitwardenProvider = func() (string, error) { t.Fatal("REPL attempted to consume terminal input"); return "", nil }
	transport := &commandTestTransport{err: errors.New(providerexec.ErrBitwardenNotAuthenticated)}
	p := &bitwardenProvider{name: "bw", transport: commandTransport(context.Background(), transport), noUnlock: true}
	_, err := p.GetRef(config.CredentialRefConfig{Ref: "item"})
	if err == nil {
		t.Fatal("missing bootstrap error")
	}
	if !transport.req.NonInteractive {
		t.Fatal("provider request allows interactive signin")
	}
}

func TestCommandRegistryUsesExplicitAgentAction(t *testing.T) {
	client := stubProviderAgent(t, nil)
	client.err = errors.New("unsupported provider action get_noninteractive")
	registry, err := NewCommandRegistry(context.Background(), testTransportConfig("op", config.CredentialProvider1Password, true, false))
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Provider("op").GetRef(config.CredentialRefConfig{Ref: "op://v/i/password", Username: "user"})
	if err == nil || len(client.reqs) != 1 || client.reqs[0].Action != "get_noninteractive" {
		t.Fatalf("err=%v requests=%v", err, client.reqs)
	}
}

func TestCanceledCommandDoesNotConnectAgent(t *testing.T) {
	stubAgentFailure(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (agentProviderTransport{autoStart: true, ctx: ctx}).ProviderRequest(providerexec.ProviderRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
