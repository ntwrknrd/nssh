package credential

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
)

type fakeOPRunner struct {
	calls []fakeOPCall
	outs  []fakeOPOut
}

type fakeOPCall struct {
	args  []string
	stdin string
}

type fakeOPOut struct {
	data string
	err  error
}

func (f *fakeOPRunner) Run(_ context.Context, stdin []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, fakeOPCall{args: append([]string(nil), args...), stdin: string(stdin)})
	if len(f.outs) == 0 {
		return nil, nil
	}
	out := f.outs[0]
	f.outs = f.outs[1:]
	return []byte(out.data), out.err
}

func TestOnePasswordGetHostWithoutConfiguredRefReturnsNil(t *testing.T) {
	client := stubProviderAgent(t, &agent.ProviderResponse{Found: true})
	provider := &onePasswordProvider{name: "op-network"}

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

func TestOnePasswordGetGroupUsesConfiguredRefThroughAgent(t *testing.T) {
	client := stubProviderAgent(t, &agent.ProviderResponse{
		Found:    true,
		Username: "netops",
		Secret:   []byte("secret"),
		Ref:      "Network Shared Admin",
	})
	provider := &onePasswordProvider{
		name: "op-network",
		groupRefs: map[string]config.CredentialRefConfig{
			"customer": {Ref: "Network Shared Admin"},
		},
	}

	got, err := provider.GetGroup("customer")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	gotSecret := revealTestSecret(t, got)
	if got == nil || got.Username != "netops" || gotSecret != "secret" || got.Ref != "Network Shared Admin" {
		t.Fatalf("record = %+v secret=%q", got, gotSecret)
	}
	req := client.reqs[0]
	if req.Provider != "op-network" || req.Action != "get" || req.Scope != "group" || req.Name != "customer" || req.Ref != "Network Shared Admin" {
		t.Fatalf("request = %+v", req)
	}
}

func TestOnePasswordGetHostSendsSecretRefsThroughAgent(t *testing.T) {
	client := stubProviderAgent(t, &agent.ProviderResponse{
		Found:    true,
		Username: "netops",
		Secret:   []byte("secret"),
		Ref:      "op://Network/Edge 01/password",
	})
	provider := &onePasswordProvider{
		name: "op-network",
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {
				Ref:         "op://Network/Edge 01/password",
				UsernameRef: "op://Network/Edge 01/username",
			},
		},
	}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	gotSecret := revealTestSecret(t, got)
	if got == nil || got.Username != "netops" || gotSecret != "secret" || got.Ref != "op://Network/Edge 01/password" {
		t.Fatalf("record = %+v secret=%q", got, gotSecret)
	}
	req := client.reqs[0]
	if req.Ref != "op://Network/Edge 01/password" || req.UsernameRef != "op://Network/Edge 01/username" {
		t.Fatalf("request = %+v", req)
	}
}

func TestOnePasswordGetHostSendsLiteralUsernameThroughAgent(t *testing.T) {
	client := stubProviderAgent(t, &agent.ProviderResponse{
		Found:    true,
		Username: "netops",
		Secret:   []byte("secret"),
		Ref:      "op://Network/Edge 01/password",
	})
	provider := &onePasswordProvider{
		name: "op-network",
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {
				Ref:      "op://Network/Edge 01/password",
				Username: "netops",
			},
		},
	}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "netops" || revealTestSecret(t, got) != "secret" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	req := client.reqs[0]
	if req.Ref != "op://Network/Edge 01/password" || req.Username != "netops" {
		t.Fatalf("request = %+v", req)
	}
}

func TestOnePasswordGetsThroughAgent(t *testing.T) {
	socketPath := fmt.Sprintf("/tmp/nssh-cred-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	restore := agent.SetSocketPathForTest(socketPath)
	defer restore()

	runner := &fakeOPRunner{outs: []fakeOPOut{{
		data: `{"title":"nssh host edge01","fields":[{"label":"username","value":"admin"},{"label":"password","value":"secret"}]}`,
	}}}
	provider := agent.NewRuntimeProvider()
	provider.Register1Password("op-network", agent.OnePasswordProviderConfig{
		Account: "ntwrknrd",
		Vault:   "Network",
		Runner:  runner,
	})
	cancel, done := agent.RunInBackground(context.Background(), provider, agent.DefaultRuntimeConfig())
	defer func() {
		cancel()
		<-done
	}()
	waitForCredentialAgent(t)

	op := &onePasswordProvider{
		name: "op-network",
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {Ref: "nssh host edge01"},
		},
	}
	got, err := op.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "admin" || revealTestSecret(t, got) != "secret" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if len(runner.calls) != 1 {
		t.Fatalf("op calls = %d, want 1", len(runner.calls))
	}
}
