package credential

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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

type fakeAgentProviderClient struct {
	reqs []agent.ProviderRequest
	resp *agent.ProviderResponse
	err  error
}

func (f *fakeAgentProviderClient) ProviderRequest(req agent.ProviderRequest) (*agent.ProviderResponse, error) {
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fakeAgentProviderClient) Close() error {
	return nil
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
	runner := &fakeOPRunner{}
	provider := &onePasswordProvider{account: "ntwrknrd", vault: "Network", runner: runner}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got != nil {
		t.Fatalf("record = %+v, want nil", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %d, want 0", len(runner.calls))
	}
}

func TestOnePasswordGetGroupReadsConfiguredItemRef(t *testing.T) {
	runner := &fakeOPRunner{outs: []fakeOPOut{{
		data: `{"title":"Network Shared Admin","fields":[{"label":"username","value":"netops"},{"label":"password","value":"secret"}]}`,
	}}}
	provider := &onePasswordProvider{
		vault: "Network",
		groupRefs: map[string]config.CredentialRefConfig{
			"custcbb": {Ref: "Network Shared Admin"},
		},
		runner: runner,
	}

	got, err := provider.GetGroup("custcbb")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	gotSecret := revealTestSecret(t, got)
	if got == nil || got.Username != "netops" || gotSecret != "secret" || got.Ref != "Network Shared Admin" {
		t.Fatalf("record = %+v secret=%q", got, gotSecret)
	}
	wantArgs := []string{"item", "get", "Network Shared Admin", "--vault", "Network", "--format", "json", "--reveal"}
	if strings.Join(runner.calls[0].args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %#v", runner.calls[0].args)
	}
}

func TestOnePasswordGetHostReadsConfiguredSecretRefs(t *testing.T) {
	runner := &fakeOPRunner{outs: []fakeOPOut{
		{data: "netops"},
		{data: "secret"},
	}}
	provider := &onePasswordProvider{
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {
				Ref:         "op://Network/Edge 01/password",
				UsernameRef: "op://Network/Edge 01/username",
			},
		},
		runner: runner,
	}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	gotSecret := revealTestSecret(t, got)
	if got == nil || got.Username != "netops" || gotSecret != "secret" || got.Ref != "op://Network/Edge 01/password" {
		t.Fatalf("record = %+v secret=%q", got, gotSecret)
	}
	if strings.Join(runner.calls[0].args, " ") != "read op://Network/Edge 01/username" {
		t.Fatalf("username ref args = %#v", runner.calls[0].args)
	}
	if strings.Join(runner.calls[1].args, " ") != "read op://Network/Edge 01/password" {
		t.Fatalf("password ref args = %#v", runner.calls[1].args)
	}
}

func TestOnePasswordAgentSessionRoutesGetThroughAgent(t *testing.T) {
	socketPath := fmt.Sprintf("/tmp/nssh-cred-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	restore := agent.SetSocketPathForTest(socketPath)
	defer restore()

	runner := &fakeOPRunner{outs: []fakeOPOut{{
		data: `{"title":"nssh host edge01","fields":[{"label":"username","value":"admin"},{"label":"password","value":"secret"}]}`,
	}}}
	provider := agent.NewRuntimeProvider()
	provider.Register1Password("op-network", agent.OnePasswordSessionConfig{
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
		name:    "op-network",
		account: "ntwrknrd",
		vault:   "Network",
		session: config.ProviderSessionAgentOwned,
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

func TestOnePasswordAgentSessionRepeatedRequestsUseAgentProcess(t *testing.T) {
	socketPath := fmt.Sprintf("/tmp/nssh-cred-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	restore := agent.SetSocketPathForTest(socketPath)
	defer restore()

	runner := &fakeOPRunner{outs: []fakeOPOut{
		{data: `{"title":"nssh host edge01","fields":[{"label":"username","value":"admin"},{"label":"password","value":"one"}]}`},
		{data: `{"title":"nssh host edge02","fields":[{"label":"username","value":"admin"},{"label":"password","value":"two"}]}`},
	}}
	provider := agent.NewRuntimeProvider()
	provider.Register1Password("op-network", agent.OnePasswordSessionConfig{Vault: "Network", Runner: runner})
	cancel, done := agent.RunInBackground(context.Background(), provider, agent.DefaultRuntimeConfig())
	defer func() {
		cancel()
		<-done
	}()
	waitForCredentialAgent(t)

	first := &onePasswordProvider{
		name:    "op-network",
		vault:   "Network",
		session: config.ProviderSessionAgentOwned,
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {Ref: "nssh host edge01"},
		},
	}
	second := &onePasswordProvider{
		name:    "op-network",
		vault:   "Network",
		session: config.ProviderSessionAgentOwned,
		hostRefs: map[string]config.CredentialRefConfig{
			"edge02": {Ref: "nssh host edge02"},
		},
	}
	if _, err := first.GetHost("edge01"); err != nil {
		t.Fatalf("first GetHost: %v", err)
	}
	if _, err := second.GetHost("edge02"); err != nil {
		t.Fatalf("second GetHost: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("op calls = %d, want 2 through same agent runner", len(runner.calls))
	}
}

func TestOnePasswordAgentSessionAutoStartsRuntimeAgent(t *testing.T) {
	restoreConnect := connectProviderAgent
	restoreSpawn := spawnRuntimeAgent
	defer func() {
		connectProviderAgent = restoreConnect
		spawnRuntimeAgent = restoreSpawn
	}()

	spawnCalls := 0
	connectCalls := 0
	client := &fakeAgentProviderClient{resp: &agent.ProviderResponse{
		Found:    true,
		Username: "admin",
		Secret:   []byte("secret"),
		Ref:      "nssh host edge01",
	}}
	connectProviderAgent = func() (agentProviderClient, error) {
		connectCalls++
		if connectCalls == 1 {
			return nil, agent.ErrAgentNotRunning
		}
		return client, nil
	}
	spawnRuntimeAgent = func() error {
		spawnCalls++
		return nil
	}

	op := &onePasswordProvider{
		name:           "op-network",
		session:        config.ProviderSessionAgentOwned,
		hostRefs:       map[string]config.CredentialRefConfig{"edge01": {Ref: "nssh host edge01"}},
		autoStartAgent: true,
	}
	got, err := op.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if spawnCalls != 1 {
		t.Fatalf("spawn calls = %d, want 1", spawnCalls)
	}
	if got == nil || got.Username != "admin" || revealTestSecret(t, got) != "secret" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if len(client.reqs) != 1 || client.reqs[0].Provider != "op-network" {
		t.Fatalf("agent requests = %+v", client.reqs)
	}
}

func TestOnePasswordAgentSessionAutoStartCanBeDisabled(t *testing.T) {
	restoreConnect := connectProviderAgent
	restoreSpawn := spawnRuntimeAgent
	defer func() {
		connectProviderAgent = restoreConnect
		spawnRuntimeAgent = restoreSpawn
	}()

	spawnCalls := 0
	connectProviderAgent = func() (agentProviderClient, error) {
		return nil, agent.ErrAgentNotRunning
	}
	spawnRuntimeAgent = func() error {
		spawnCalls++
		return nil
	}

	op := &onePasswordProvider{
		name:           "op-network",
		session:        config.ProviderSessionAgentOwned,
		hostRefs:       map[string]config.CredentialRefConfig{"edge01": {Ref: "nssh host edge01"}},
		autoStartAgent: false,
	}
	_, err := op.GetHost("edge01")
	if !errors.Is(err, agent.ErrAgentNotRunning) {
		t.Fatalf("GetHost error = %v, want ErrAgentNotRunning", err)
	}
	if spawnCalls != 0 {
		t.Fatalf("spawn calls = %d, want 0", spawnCalls)
	}
}

func waitForCredentialAgent(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client, err := agent.Connect()
		if err == nil {
			_ = client.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent did not start in time")
}

func revealTestSecret(t *testing.T, record *Record) string {
	t.Helper()
	if record == nil || record.Secret == nil {
		return ""
	}
	value := ""
	if err := record.Secret.Use(func(pw []byte) error {
		value = string(pw)
		return nil
	}); err != nil {
		t.Fatalf("secret: %v", err)
	}
	return value
}
