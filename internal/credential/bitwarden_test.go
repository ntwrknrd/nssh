package credential

import (
	"errors"
	"testing"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
)

func TestBitwardenGetHostUsesConfiguredRefThroughAgent(t *testing.T) {
	client := stubProviderAgent(t, &agent.ProviderResponse{
		Found:    true,
		Username: "admin",
		Secret:   []byte("secret"),
		Ref:      "Existing Edge Item",
	})

	provider := &bitwardenProvider{
		name: "bw-lab",
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {Ref: "Existing Edge Item"},
		},
	}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Ref != "Existing Edge Item" || got.Username != "admin" {
		t.Fatalf("record = %+v", got)
	}
	if len(client.reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(client.reqs))
	}
	req := client.reqs[0]
	if req.Provider != "bw-lab" || req.Action != "get" || req.Scope != "host" || req.Name != "edge01" || req.Ref != "Existing Edge Item" {
		t.Fatalf("request = %+v", req)
	}
}

func TestBitwardenGetHostWithoutConfiguredRefReturnsNil(t *testing.T) {
	client := stubProviderAgent(t, &agent.ProviderResponse{Found: true})
	provider := &bitwardenProvider{name: "bw-lab"}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got != nil {
		t.Fatalf("record = %+v, want nil", got)
	}
	if len(client.reqs) != 0 {
		t.Fatalf("requests = %d, want 0", len(client.reqs))
	}
}

func TestBitwardenMissingItemReturnsNil(t *testing.T) {
	stubProviderAgent(t, &agent.ProviderResponse{Found: false})
	provider := &bitwardenProvider{
		name: "bw-lab",
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {Ref: "missing item"},
		},
	}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got != nil {
		t.Fatalf("record = %+v, want nil", got)
	}
}

func TestBitwardenLookupUnlocksAndRetriesWhenAgentNeedsSession(t *testing.T) {
	oldConnect := connectProviderAgent
	oldSpawn := spawnRuntimeAgent
	oldUnlock := unlockBitwardenProvider
	defer func() {
		connectProviderAgent = oldConnect
		spawnRuntimeAgent = oldSpawn
		unlockBitwardenProvider = oldUnlock
	}()

	client := &fakeAgentProviderClient{
		errs: []error{
			errors.New(agent.ErrBitwardenNotAuthenticated),
		},
		responses: []*agent.ProviderResponse{
			{
				Found:    true,
				Username: "admin",
				Secret:   []byte("secret"),
				Ref:      "Existing Edge Item",
			},
		},
	}
	connectProviderAgent = func() (agentProviderClient, error) { return client, nil }
	spawnRuntimeAgent = func() error { t.Fatal("unexpected agent spawn"); return nil }

	var unlockCalls int
	unlockBitwardenProvider = func() (string, error) {
		unlockCalls++
		return "bw-session-token", nil
	}

	provider := &bitwardenProvider{
		name: "bw-lab",
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {Ref: "Existing Edge Item"},
		},
	}
	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "admin" || revealTestSecret(t, got) != "secret" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if unlockCalls != 1 {
		t.Fatalf("unlock calls = %d, want 1", unlockCalls)
	}
	if len(client.reqs) != 2 {
		t.Fatalf("requests = %+v, want get/get", client.reqs)
	}
	if client.reqs[0].Action != "get" || client.reqs[1].Action != "get" {
		t.Fatalf("request order = %+v", client.reqs)
	}
	if client.reqs[1].Provider != "bw-lab" || client.reqs[1].Session != "bw-session-token" {
		t.Fatalf("request-scoped get = %+v", client.reqs[1])
	}
}
