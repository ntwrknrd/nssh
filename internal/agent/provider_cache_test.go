//go:build linux || darwin

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/sopsdoc"
)

func TestNewConfiguredRuntimeProviderRegistersAllCredentialProviders(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"op-network": {
			Type: config.CredentialProvider1Password,
			Config: config.CredentialProviderDetailConfig{
				Vault: "Network",
			},
		},
		"op-work": {
			Type: config.CredentialProvider1Password,
			Config: config.CredentialProviderDetailConfig{
				Vault: "Work",
			},
		},
		"sops": {
			Type: config.CredentialProviderSOPSAge,
			File: "/tmp/credentials.sops.yaml",
		},
		"bw-lab": {
			Type: config.CredentialProviderBitwarden,
		},
		"bw-external": {
			Type: config.CredentialProviderBitwarden,
		},
	}

	provider := NewConfiguredRuntimeProvider(cfg)
	if provider.ProviderCount() != 5 {
		t.Fatalf("provider count = %d, want 5", provider.ProviderCount())
	}
	names := provider.ProviderNames()
	if strings.Join(names, ",") != "bw-external,bw-lab,op-network,op-work,sops" {
		t.Fatalf("provider names = %v", names)
	}
}

func TestNewConfiguredRuntimeProviderRegistersOnePassword(t *testing.T) {
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
	if provider.ProviderCount() != 1 {
		t.Fatalf("provider count = %d, want 1", provider.ProviderCount())
	}
}

type fakeCredentialBroker struct {
	requests   []ProviderRequest
	responses  []ProviderResponse
	closeCount int
}

type deadlineCheckingBroker struct {
	deadline time.Time
}

type fakeOnePasswordRunner struct {
	calls [][]string
	outs  [][]byte
	errs  []error
}

type fakeSOPSAgeRunner struct {
	calls int
	out   []byte
}

type fakeBitwardenRunner struct {
	calls []fakeBitwardenCall
	out   []byte
}

type fakeBitwardenCall struct {
	env  []string
	args []string
}

func (f *fakeOnePasswordRunner) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	var out []byte
	if len(f.outs) > 0 {
		out = f.outs[0]
		f.outs = f.outs[1:]
	}
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func (f *fakeSOPSAgeRunner) Run(_ context.Context, _ []string, _ ...string) ([]byte, error) {
	f.calls++
	return f.out, nil
}

func (f *fakeBitwardenRunner) Run(_ context.Context, env []string, _ []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, fakeBitwardenCall{env: append([]string(nil), env...), args: append([]string(nil), args...)})
	return f.out, nil
}

func (p *fakeCredentialBroker) HandleProviderRequest(_ context.Context, req ProviderRequest) (ProviderResponse, error) {
	p.requests = append(p.requests, req)
	if len(p.responses) == 0 {
		return ProviderResponse{}, nil
	}
	resp := p.responses[0]
	p.responses = p.responses[1:]
	return resp, nil
}

func (p *fakeCredentialBroker) Close() error {
	p.closeCount++
	return nil
}

func (p *deadlineCheckingBroker) HandleProviderRequest(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return ProviderResponse{}, errors.New("missing provider request deadline")
	}
	p.deadline = deadline
	return ProviderResponse{Found: true}, nil
}

func (p *deadlineCheckingBroker) Close() error {
	return nil
}

func TestRuntimeProviderResolvesOnePasswordSecretRefs(t *testing.T) {
	runner := &fakeOnePasswordRunner{outs: [][]byte{
		[]byte("netops\n"),
		[]byte("secret\n"),
	}}
	provider := NewRuntimeProvider()
	provider.Register1Password("op-network", OnePasswordProviderConfig{
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

func TestRuntimeProviderOnePasswordSecretRefSignsInAndRetries(t *testing.T) {
	runner := &fakeOnePasswordRunner{
		outs: [][]byte{
			nil,
			nil,
			[]byte("secret\n"),
		},
		errs: []error{
			errors.New("op read failed: account is not signed in"),
			nil,
			nil,
		},
	}
	provider := NewRuntimeProvider()
	provider.Register1Password("op-network", OnePasswordProviderConfig{
		Account: "ntwrknrd",
		Runner:  runner,
	})

	resp, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "op-network",
		Action:   "get",
		Ref:      "op://Network/Edge/password",
		Username: "netops",
	})
	if err != nil {
		t.Fatalf("HandleProviderRequest: %v", err)
	}
	if !resp.Found || resp.Username != "netops" || string(resp.Secret) != "secret" {
		t.Fatalf("response = %+v", resp)
	}
	got := make([]string, 0, len(runner.calls))
	for _, call := range runner.calls {
		got = append(got, strings.Join(call, " "))
	}
	want := "read op://Network/Edge/password --account ntwrknrd\nsignin --account ntwrknrd\nread op://Network/Edge/password --account ntwrknrd"
	if strings.Join(got, "\n") != want {
		t.Fatalf("op calls = %q, want %q", strings.Join(got, "\n"), want)
	}
}

func TestRuntimeProviderOnePasswordItemGetSignsInAndRetries(t *testing.T) {
	item := []byte(`{"title":"Edge","fields":[{"label":"username","value":"netops"},{"label":"password","value":"secret"}]}`)
	runner := &fakeOnePasswordRunner{
		outs: [][]byte{
			nil,
			nil,
			item,
		},
		errs: []error{
			errors.New("op item get failed: account is not signed in"),
			nil,
			nil,
		},
	}
	provider := NewRuntimeProvider()
	provider.Register1Password("op-network", OnePasswordProviderConfig{
		Account: "ntwrknrd",
		Vault:   "Network",
		Runner:  runner,
	})

	resp, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "op-network",
		Action:   "get",
		Ref:      "Edge",
	})
	if err != nil {
		t.Fatalf("HandleProviderRequest: %v", err)
	}
	if !resp.Found || resp.Username != "netops" || string(resp.Secret) != "secret" {
		t.Fatalf("response = %+v", resp)
	}
	got := make([]string, 0, len(runner.calls))
	for _, call := range runner.calls {
		got = append(got, strings.Join(call, " "))
	}
	want := "item get Edge --vault Network --account ntwrknrd --format json --reveal\nsignin --account ntwrknrd\nitem get Edge --vault Network --account ntwrknrd --format json --reveal"
	if strings.Join(got, "\n") != want {
		t.Fatalf("op calls = %q, want %q", strings.Join(got, "\n"), want)
	}
}

func TestRuntimeProviderResolvesOnePasswordItemBaseRefs(t *testing.T) {
	runner := &fakeOnePasswordRunner{outs: [][]byte{
		[]byte("netops\n"),
		[]byte("secret\n"),
	}}
	provider := NewRuntimeProvider()
	provider.Register1Password("op-network", OnePasswordProviderConfig{
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

func TestRuntimeProviderOnePasswordKeepaliveArmsAfterSuccessfulGet(t *testing.T) {
	runner := &fakeOnePasswordRunner{outs: [][]byte{
		[]byte("netops\n"),
		[]byte("secret\n"),
		[]byte("user@example.com\n"),
	}}
	provider := NewRuntimeProvider()
	provider.Register1Password("op-network", OnePasswordProviderConfig{
		Account:           "ntwrknrd",
		Runner:            runner,
		Keepalive:         true,
		KeepaliveInterval: 10 * time.Millisecond,
		KeepaliveTimeout:  5 * time.Millisecond,
	})

	resp, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "op-network",
		Action:   "get",
		Ref:      "op://Network/Edge/",
	})
	if err != nil {
		t.Fatalf("HandleProviderRequest: %v", err)
	}
	if !resp.Found {
		t.Fatal("response not found")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(runner.calls) >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(runner.calls) < 3 {
		t.Fatalf("op calls = %v, want keepalive tick", runner.calls)
	}
	if got := strings.Join(runner.calls[2], " "); got != "whoami --account ntwrknrd" {
		t.Fatalf("keepalive args = %q", got)
	}
	entries := provider.AccessStatus()
	if len(entries) != 1 || entries[0].Name != "op-network" || entries[0].OnePasswordState != "active" {
		t.Fatalf("access status = %+v", entries)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestRuntimeProviderOnePasswordKeepaliveSuspendsAfterFailure(t *testing.T) {
	runner := &fakeOnePasswordRunner{
		outs: [][]byte{
			[]byte("netops\n"),
			[]byte("secret\n"),
		},
		errs: []error{nil, nil, errors.New("op whoami failed: secret stdout")},
	}
	provider := NewRuntimeProvider()
	provider.Register1Password("op-network", OnePasswordProviderConfig{
		Runner:            runner,
		Keepalive:         true,
		KeepaliveInterval: 10 * time.Millisecond,
		KeepaliveTimeout:  5 * time.Millisecond,
	})

	if _, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "op-network",
		Action:   "get",
		Ref:      "op://Network/Edge/",
	}); err != nil {
		t.Fatalf("HandleProviderRequest: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	var entries []AccessStatus
	for time.Now().Before(deadline) {
		entries = provider.AccessStatus()
		if len(entries) == 1 && entries[0].OnePasswordState == "suspended" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(entries) != 1 || entries[0].OnePasswordState != "suspended" {
		t.Fatalf("access status = %+v, want suspended", entries)
	}
	if strings.Contains(entries[0].LastError, "secret stdout") {
		t.Fatalf("last error leaked provider output: %+v", entries[0])
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestRuntimeProviderAccessStatusOmitsProvidersWithoutManagedAccess(t *testing.T) {
	provider := NewRuntimeProvider()
	provider.RegisterSOPSAge("sops", SOPSAgeProviderConfig{File: "/tmp/credentials.sops.yaml", Runner: &fakeSOPSAgeRunner{}})
	provider.Register1Password("op-plain", OnePasswordProviderConfig{Vault: "Network", Runner: &fakeOnePasswordRunner{}})
	provider.RegisterBitwarden("bw-plain", BitwardenProviderConfig{Runner: &fakeBitwardenRunner{}})

	if entries := provider.AccessStatus(); len(entries) != 0 {
		t.Fatalf("access status = %+v, want no unmanaged providers", entries)
	}
}

func TestRuntimeProviderAccessStatusShowsWarmBitwardenWithoutSessionValue(t *testing.T) {
	provider := NewRuntimeProvider()
	provider.RegisterBitwarden("bw-work", BitwardenProviderConfig{Runner: &fakeBitwardenRunner{}, WarmSession: true})
	if _, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-work",
		Action:   "auth",
		Session:  "bw-secret-session",
	}); err != nil {
		t.Fatalf("auth: %v", err)
	}

	entries := provider.AccessStatus()
	if len(entries) != 1 {
		t.Fatalf("access status = %+v, want one entry", entries)
	}
	got := entries[0]
	if got.Name != "bw-work" || got.Type != config.CredentialProviderBitwarden || !got.BitwardenWarmSession || !got.BitwardenWarmActive {
		t.Fatalf("entry = %+v", got)
	}
	if strings.Contains(fmt.Sprintf("%+v", got), "bw-secret-session") {
		t.Fatalf("access status leaked session: %+v", got)
	}
}

func TestRuntimeProviderResolvesSOPSAgeRefs(t *testing.T) {
	runner := &fakeSOPSAgeRunner{out: []byte(`{"expedient":{"username":"cj","password":"secret"}}`)}
	provider := NewRuntimeProvider()
	provider.RegisterSOPSAge("sops", SOPSAgeProviderConfig{
		File:   "/tmp/credentials.sops.yaml",
		Runner: runner,
	})

	resp, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider:    "sops",
		Action:      "get",
		Ref:         "expedient.password",
		UsernameRef: "expedient.username",
	})
	if err != nil {
		t.Fatalf("HandleProviderRequest: %v", err)
	}
	if !resp.Found || resp.Username != "cj" || string(resp.Secret) != "secret" || resp.Ref != "expedient.password" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestRuntimeProviderSOPSAgeDecryptsPerRequest(t *testing.T) {
	runner := &fakeSOPSAgeRunner{out: []byte(`{"expedient":{"username":"cj","password":"secret"}}`)}
	provider := NewRuntimeProvider()
	provider.RegisterSOPSAge("sops", SOPSAgeProviderConfig{
		File:   "/tmp/credentials.sops.yaml",
		Runner: runner,
	})

	for _, ref := range []string{"expedient.password", "expedient.username"} {
		if _, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
			Provider: "sops",
			Action:   "get",
			Ref:      ref,
		}); err != nil {
			t.Fatalf("HandleProviderRequest(%s): %v", ref, err)
		}
	}
	if runner.calls != 2 {
		t.Fatalf("decrypt calls = %d, want 2", runner.calls)
	}
}

func TestRuntimeProviderBitwardenAuthStoresSessionOnlyWhenWarmSessionEnabled(t *testing.T) {
	runner := &fakeBitwardenRunner{out: []byte(`{"name":"edge item","login":{"username":"netops","password":"secret"}}`)}
	provider := NewRuntimeProvider()
	provider.RegisterBitwarden("bw-lab", BitwardenProviderConfig{Runner: runner, WarmSession: true})

	if _, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "auth",
		Session:  "bw-session-token",
	}); err != nil {
		t.Fatalf("auth: %v", err)
	}
	resp, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "get",
		Ref:      "edge item",
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !resp.Found || resp.Username != "netops" || string(resp.Secret) != "secret" {
		t.Fatalf("response = %+v", resp)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(runner.calls))
	}
	if strings.Join(runner.calls[0].env, ",") != "BW_SESSION=bw-session-token" {
		t.Fatalf("env = %v", runner.calls[0].env)
	}
	if strings.Join(runner.calls[0].args, " ") != "get item edge item" {
		t.Fatalf("args = %v", runner.calls[0].args)
	}
}

func TestRuntimeProviderBitwardenAuthDoesNotStoreSessionByDefault(t *testing.T) {
	provider := NewRuntimeProvider()
	provider.RegisterBitwarden("bw-lab", BitwardenProviderConfig{Runner: &fakeBitwardenRunner{}})

	if _, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "auth",
		Session:  "bw-session-token",
	}); err != nil {
		t.Fatalf("auth: %v", err)
	}
	_, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "get",
		Ref:      "edge item",
	})
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("error = %v, want not authenticated", err)
	}
}

func TestRuntimeProviderBitwardenGetUsesRequestScopedSessionWithoutStoringIt(t *testing.T) {
	runner := &fakeBitwardenRunner{out: []byte(`{"name":"edge item","login":{"username":"netops","password":"secret"}}`)}
	provider := NewRuntimeProvider()
	provider.RegisterBitwarden("bw-lab", BitwardenProviderConfig{Runner: runner})

	resp, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "get",
		Ref:      "edge item",
		Session:  "request-session-token",
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !resp.Found || string(resp.Secret) != "secret" {
		t.Fatalf("response = %+v", resp)
	}
	if strings.Join(runner.calls[0].env, ",") != "BW_SESSION=request-session-token" {
		t.Fatalf("env = %v", runner.calls[0].env)
	}

	_, err = provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "get",
		Ref:      "edge item",
	})
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("error = %v, want not authenticated after request-scoped get", err)
	}
}

func TestRuntimeProviderBitwardenGetRequiresAuth(t *testing.T) {
	provider := NewRuntimeProvider()
	provider.RegisterBitwarden("bw-lab", BitwardenProviderConfig{Runner: &fakeBitwardenRunner{}})

	_, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "get",
		Ref:      "edge item",
	})
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("error = %v, want not authenticated", err)
	}
}

func TestRuntimeProviderCloseClearsBitwardenSession(t *testing.T) {
	provider := NewRuntimeProvider()
	provider.RegisterBitwarden("bw-lab", BitwardenProviderConfig{Runner: &fakeBitwardenRunner{}})
	if _, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "auth",
		Session:  "bw-session-token",
	}); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "get",
		Ref:      "edge item",
	})
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("error = %v, want not authenticated", err)
	}
}

func TestAgentServesProviderRequests(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	provider := &fakeCredentialBroker{responses: []ProviderResponse{{
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

func TestAgentProviderRequestsUseConfiguredDeadline(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	provider := &deadlineCheckingBroker{}
	cfg := DefaultRuntimeConfig()
	cfg.Agent.ProviderRequestTimeout = config.Duration(30 * time.Second)
	cancel, done := RunInBackground(context.Background(), provider, cfg)
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

	before := time.Now()
	if _, err := client.ProviderRequest(ProviderRequest{Provider: "op-network", Action: "get", Ref: "edge"}); err != nil {
		t.Fatalf("ProviderRequest: %v", err)
	}
	until := provider.deadline.Sub(before)
	if until < 20*time.Second || until > 40*time.Second {
		t.Fatalf("provider request deadline offset = %v, want about 30s", until)
	}
}

func TestAgentStatusReportsManagedAccessWithoutSecrets(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	provider := NewRuntimeProvider()
	provider.RegisterSOPSAge("sops", SOPSAgeProviderConfig{File: "/tmp/credentials.sops.yaml", Runner: &fakeSOPSAgeRunner{}})
	provider.RegisterBitwarden("bw-work", BitwardenProviderConfig{Runner: &fakeBitwardenRunner{}, WarmSession: true})
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
	if _, err := client.ProviderRequest(ProviderRequest{
		Provider: "bw-work",
		Action:   "auth",
		Session:  "bw-secret-session",
	}); err != nil {
		t.Fatalf("auth: %v", err)
	}
	status, err := client.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Access) != 1 {
		t.Fatalf("access = %+v, want one Bitwarden warm access entry", status.Access)
	}
	if status.Access[0].Name != "bw-work" || !status.Access[0].BitwardenWarmActive {
		t.Fatalf("access = %+v", status.Access)
	}
	if strings.Contains(fmt.Sprintf("%+v", status), "sops") || strings.Contains(fmt.Sprintf("%+v", status), "bw-secret-session") {
		t.Fatalf("status leaked unmanaged provider or session: %+v", status)
	}
}

func TestAgentStatusReportsRuntimeAndResourceState(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	cancel, done := RunInBackground(context.Background(), NewRuntimeProvider(), DefaultRuntimeConfig())
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
	status, err := client.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version = %d, want %d", status.ProtocolVersion, ProtocolVersion)
	}
	if status.PID <= 0 {
		t.Fatalf("pid = %d, want real pid", status.PID)
	}
	if status.SocketPath != socketPath {
		t.Fatalf("socket path = %q, want %q", status.SocketPath, socketPath)
	}
	if status.PeerVerification == "" {
		t.Fatal("peer verification mode is empty")
	}
	if status.UptimeSeconds < 0 {
		t.Fatalf("uptime = %d, want non-negative", status.UptimeSeconds)
	}
	if status.ProcessCount != 1 || status.DuplicateProcesses {
		t.Fatalf("process state count=%d duplicate=%v, want one process", status.ProcessCount, status.DuplicateProcesses)
	}
	if status.HeapAllocBytes == 0 || status.Goroutines == 0 {
		t.Fatalf("resource state heap=%d goroutines=%d", status.HeapAllocBytes, status.Goroutines)
	}
	if status.OpenFDs < -1 {
		t.Fatalf("open fds = %d, want known count or -1 unknown", status.OpenFDs)
	}
}

var _ sopsdoc.Runner = (*fakeSOPSAgeRunner)(nil)
