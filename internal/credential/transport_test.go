package credential

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/providerexec"
)

type fakeProviderExecutor struct {
	reqs      []providerexec.ProviderRequest
	responses []providerexec.ProviderResponse
	errs      []error
}

func (f *fakeProviderExecutor) HandleProviderRequest(ctx context.Context, req providerexec.ProviderRequest) (providerexec.ProviderResponse, error) {
	if _, ok := ctx.Deadline(); !ok {
		return providerexec.ProviderResponse{}, errors.New("missing direct provider deadline")
	}
	f.reqs = append(f.reqs, req)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return providerexec.ProviderResponse{}, err
	}
	if len(f.responses) == 0 {
		return providerexec.ProviderResponse{}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func stubAgentFailure(t *testing.T) {
	t.Helper()
	oldConnect := connectProviderAgent
	oldSpawn := spawnRuntimeAgent
	connectProviderAgent = func() (agentProviderClient, error) {
		t.Fatal("unexpected agent connect")
		return nil, nil
	}
	spawnRuntimeAgent = func() error {
		t.Fatal("unexpected agent spawn")
		return nil
	}
	t.Cleanup(func() {
		connectProviderAgent = oldConnect
		spawnRuntimeAgent = oldSpawn
	})
}

func TestRegistryUsesDirectTransportForSOPSAge(t *testing.T) {
	stubAgentFailure(t)
	executor := &fakeProviderExecutor{responses: []providerexec.ProviderResponse{{
		Found:    true,
		Username: "cj",
		Secret:   []byte("secret"),
		Ref:      "expedient.password",
	}}}
	oldNewExecutor := newConfiguredProviderExecutor
	newConfiguredProviderExecutor = func(*config.Config) providerRequestExecutor { return executor }
	t.Cleanup(func() { newConfiguredProviderExecutor = oldNewExecutor })

	registry, err := NewRegistry(testTransportConfig("sops", config.CredentialProviderSOPSAge, false, false))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	got, err := registry.Provider("sops").GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "cj" || revealTestSecret(t, got) != "secret" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if len(executor.reqs) != 1 || executor.reqs[0].Provider != "sops" || executor.reqs[0].Ref != "hosts.edge01.password" {
		t.Fatalf("direct requests = %+v", executor.reqs)
	}
}

func TestRegistryUsesDirectTransportForOnePasswordWithoutKeepalive(t *testing.T) {
	stubAgentFailure(t)
	executor := &fakeProviderExecutor{responses: []providerexec.ProviderResponse{{
		Found:    true,
		Username: "netops",
		Secret:   []byte("secret"),
		Ref:      "op://Network/Edge/password",
	}}}
	oldNewExecutor := newConfiguredProviderExecutor
	newConfiguredProviderExecutor = func(*config.Config) providerRequestExecutor { return executor }
	t.Cleanup(func() { newConfiguredProviderExecutor = oldNewExecutor })

	registry, err := NewRegistry(testTransportConfig("op-network", config.CredentialProvider1Password, false, false))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	got, err := registry.Provider("op-network").GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "netops" || revealTestSecret(t, got) != "secret" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if len(executor.reqs) != 1 || executor.reqs[0].Provider != "op-network" || executor.reqs[0].Ref != "hosts.edge01.password" {
		t.Fatalf("direct requests = %+v", executor.reqs)
	}
}

func TestRegistryUsesAgentTransportForOnePasswordKeepalive(t *testing.T) {
	client := stubProviderAgent(t, &agent.ProviderResponse{
		Found:    true,
		Username: "netops",
		Secret:   []byte("secret"),
		Ref:      "hosts.edge01.password",
	})
	executor := &fakeProviderExecutor{}
	oldNewExecutor := newConfiguredProviderExecutor
	newConfiguredProviderExecutor = func(*config.Config) providerRequestExecutor { return executor }
	t.Cleanup(func() { newConfiguredProviderExecutor = oldNewExecutor })

	registry, err := NewRegistry(testTransportConfig("op-network", config.CredentialProvider1Password, true, false))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	got, err := registry.Provider("op-network").GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "netops" || revealTestSecret(t, got) != "secret" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if len(executor.reqs) != 0 {
		t.Fatalf("direct requests = %+v, want none", executor.reqs)
	}
	if len(client.reqs) != 1 || client.reqs[0].Provider != "op-network" {
		t.Fatalf("agent requests = %+v", client.reqs)
	}
}

func TestRegistryUsesDirectTransportForBitwardenWithoutWarmSession(t *testing.T) {
	stubAgentFailure(t)
	oldUnlock := unlockBitwardenProvider
	unlockBitwardenProvider = func() (string, error) { return "bw-session-token", nil }
	t.Cleanup(func() { unlockBitwardenProvider = oldUnlock })
	executor := &fakeProviderExecutor{
		errs: []error{errors.New(providerexec.ErrBitwardenNotAuthenticated)},
		responses: []providerexec.ProviderResponse{{
			Found:    true,
			Username: "admin",
			Secret:   []byte("secret"),
			Ref:      "Existing Edge Item",
		}},
	}
	oldNewExecutor := newConfiguredProviderExecutor
	newConfiguredProviderExecutor = func(*config.Config) providerRequestExecutor { return executor }
	t.Cleanup(func() { newConfiguredProviderExecutor = oldNewExecutor })

	registry, err := NewRegistry(testTransportConfig("bw-lab", config.CredentialProviderBitwarden, false, false))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	got, err := registry.Provider("bw-lab").GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "admin" || revealTestSecret(t, got) != "secret" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if len(executor.reqs) != 2 || executor.reqs[1].Session != "bw-session-token" {
		t.Fatalf("direct requests = %+v", executor.reqs)
	}
}

func TestRegistryUsesAgentTransportForBitwardenWarmSession(t *testing.T) {
	client := stubProviderAgent(t, &agent.ProviderResponse{
		Found:    true,
		Username: "admin",
		Secret:   []byte("secret"),
		Ref:      "Existing Edge Item",
	})
	executor := &fakeProviderExecutor{}
	oldNewExecutor := newConfiguredProviderExecutor
	newConfiguredProviderExecutor = func(*config.Config) providerRequestExecutor { return executor }
	t.Cleanup(func() { newConfiguredProviderExecutor = oldNewExecutor })

	registry, err := NewRegistry(testTransportConfig("bw-lab", config.CredentialProviderBitwarden, false, true))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	got, err := registry.Provider("bw-lab").GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "admin" || revealTestSecret(t, got) != "secret" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if len(executor.reqs) != 0 {
		t.Fatalf("direct requests = %+v, want none", executor.reqs)
	}
	if len(client.reqs) != 1 || client.reqs[0].Provider != "bw-lab" {
		t.Fatalf("agent requests = %+v", client.reqs)
	}
}

func testTransportConfig(providerName, providerType string, keepalive, warmSession bool) *config.Config {
	provider := config.CredentialProviderConfig{
		Type:        providerType,
		Keepalive:   keepalive,
		WarmSession: warmSession,
		File:        "/tmp/credentials.sops.yaml",
		Config: config.CredentialProviderDetailConfig{
			Vault: "Network",
		},
	}
	return &config.Config{
		Agent: config.AgentConfig{
			AutoStart:              true,
			ProviderRequestTimeout: config.Duration(90 * time.Second),
		},
		Credential: config.CredentialConfig{
			Provider: map[string]config.CredentialProviderConfig{
				providerName: provider,
			},
		},
		Inventory: config.InventoryConfig{
			Host: map[string]config.InventoryHostConfig{
				"edge01": {Auth: config.InventoryAuthConfig{
					CredentialProvider: providerName,
					PasswordRef:        "hosts.edge01.password",
				}},
			},
		},
	}
}
