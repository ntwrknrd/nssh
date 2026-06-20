//go:build linux || darwin

package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestNewConfiguredRuntimeProviderRegistersAgentOwned1PasswordProviders(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"op-network": {
			Type: config.CredentialProvider1Password,
			Config: config.CredentialProviderDetailConfig{
				Vault:   "Network",
				Session: config.ProviderSessionAgentOwned,
			},
		},
		"op-work": {
			Type: config.CredentialProvider1Password,
			Config: config.CredentialProviderDetailConfig{
				Vault:   "Work",
				Session: config.ProviderSessionExternal,
			},
		},
		"pass": {
			Type: config.CredentialProviderPass,
		},
	}

	provider := NewConfiguredRuntimeProvider(cfg)
	if provider.SessionCount() != 1 {
		t.Fatalf("session count = %d, want 1", provider.SessionCount())
	}
	names := provider.SessionNames()
	if len(names) != 1 || names[0] != "op-network" {
		t.Fatalf("session names = %v, want [op-network]", names)
	}
}

func TestNewConfiguredRuntimeProviderTreatsBlank1PasswordSessionAsAgentOwned(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"op-network": {
			Type: config.CredentialProvider1Password,
			Config: config.CredentialProviderDetailConfig{
				Vault: "Network",
			},
		},
	}

	provider := NewConfiguredRuntimeProvider(cfg)
	if provider.SessionCount() != 1 {
		t.Fatalf("session count = %d, want 1", provider.SessionCount())
	}
}

type fakeSessionProvider struct {
	requests   []ProviderRequest
	responses  []ProviderResponse
	closeCount int
}

type fakeOnePasswordRunner struct {
	calls [][]string
	outs  [][]byte
}

func (f *fakeOnePasswordRunner) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if len(f.outs) == 0 {
		return nil, nil
	}
	out := f.outs[0]
	f.outs = f.outs[1:]
	return out, nil
}

func (p *fakeSessionProvider) HandleProviderRequest(_ context.Context, req ProviderRequest) (ProviderResponse, error) {
	p.requests = append(p.requests, req)
	if len(p.responses) == 0 {
		return ProviderResponse{}, nil
	}
	resp := p.responses[0]
	p.responses = p.responses[1:]
	return resp, nil
}

func (p *fakeSessionProvider) Close() error {
	p.closeCount++
	return nil
}

func TestRuntimeProviderResolvesOnePasswordSecretRefs(t *testing.T) {
	runner := &fakeOnePasswordRunner{outs: [][]byte{
		[]byte("netops\n"),
		[]byte("secret\n"),
	}}
	provider := NewRuntimeProvider()
	provider.Register1Password("op-network", OnePasswordSessionConfig{
		Account: "ntwrknrd",
		Runner:  runner,
	})

	resp, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider:    "op-network",
		Action:      "get",
		Ref:         "op://Network/Edge/password",
		UsernameRef: "op://Network/Edge/username",
	})
	if err != nil {
		t.Fatalf("HandleProviderRequest: %v", err)
	}
	if !resp.Found || resp.Username != "netops" || string(resp.Secret) != "secret" {
		t.Fatalf("response = %+v", resp)
	}
	got := strings.Join(runner.calls[0], " ")
	if got != "read op://Network/Edge/username --account ntwrknrd" {
		t.Fatalf("username ref args = %q", got)
	}
	got = strings.Join(runner.calls[1], " ")
	if got != "read op://Network/Edge/password --account ntwrknrd" {
		t.Fatalf("password ref args = %q", got)
	}
}

func TestRuntimeProviderResolvesOnePasswordItemBaseRefs(t *testing.T) {
	runner := &fakeOnePasswordRunner{outs: [][]byte{
		[]byte("netops\n"),
		[]byte("secret\n"),
	}}
	provider := NewRuntimeProvider()
	provider.Register1Password("op-network", OnePasswordSessionConfig{
		Account: "ntwrknrd",
		Runner:  runner,
	})

	resp, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "op-network",
		Action:   "get",
		Ref:      "op://Network/Edge/",
	})
	if err != nil {
		t.Fatalf("HandleProviderRequest: %v", err)
	}
	if !resp.Found || resp.Username != "netops" || string(resp.Secret) != "secret" {
		t.Fatalf("response = %+v", resp)
	}
	got := strings.Join(runner.calls[0], " ")
	if got != "read op://Network/Edge/username --account ntwrknrd" {
		t.Fatalf("username ref args = %q", got)
	}
	got = strings.Join(runner.calls[1], " ")
	if got != "read op://Network/Edge/password --account ntwrknrd" {
		t.Fatalf("password ref args = %q", got)
	}
}

func TestAgentServesProviderRequests(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	provider := &fakeSessionProvider{responses: []ProviderResponse{{
		Username: "admin",
		Secret:   []byte("secret"),
		Ref:      "nssh host edge01",
	}}}
	cancel, done := RunInBackground(context.Background(), provider, DefaultRuntimeConfig())
	defer func() {
		cancel()
		<-done
	}()
	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = client.Close() }()

	resp, err := client.ProviderRequest(ProviderRequest{Provider: "op-network", Scope: "host", Name: "edge01", Action: "get"})
	if err != nil {
		t.Fatalf("ProviderRequest: %v", err)
	}
	if resp.Username != "admin" || string(resp.Secret) != "secret" || resp.Ref != "nssh host edge01" {
		t.Fatalf("response = %+v", resp)
	}
	if len(provider.requests) != 1 || provider.requests[0].Provider != "op-network" {
		t.Fatalf("provider requests = %+v", provider.requests)
	}
}
