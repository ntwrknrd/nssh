package providerexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

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

func TestExecutorResolvesOnePasswordSecretRefs(t *testing.T) {
	runner := &fakeOnePasswordRunner{outs: [][]byte{
		[]byte("netops\n"),
		[]byte("secret\n"),
	}}
	executor := NewExecutor()
	executor.Register1Password("op-network", OnePasswordProviderConfig{
		Account: "ntwrknrd",
		Runner:  runner,
	})

	resp, err := executor.HandleProviderRequest(context.Background(), ProviderRequest{
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

func TestExecutorOnePasswordSignsInAndRetriesUserRequest(t *testing.T) {
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
	executor := NewExecutor()
	executor.Register1Password("op-network", OnePasswordProviderConfig{
		Account: "ntwrknrd",
		Runner:  runner,
	})

	resp, err := executor.HandleProviderRequest(context.Background(), ProviderRequest{
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

func TestExecutorSOPSAgeDecryptsPerRequest(t *testing.T) {
	runner := &fakeSOPSAgeRunner{out: []byte(`{"expedient":{"username":"cj","password":"secret"}}`)}
	executor := NewExecutor()
	executor.RegisterSOPSAge("sops", SOPSAgeProviderConfig{
		File:   "/tmp/credentials.sops.yaml",
		Runner: runner,
	})

	for _, ref := range []string{"expedient.password", "expedient.password"} {
		resp, err := executor.HandleProviderRequest(context.Background(), ProviderRequest{
			Provider:    "sops",
			Action:      "get",
			Ref:         ref,
			UsernameRef: "expedient.username",
		})
		if err != nil {
			t.Fatalf("HandleProviderRequest(%s): %v", ref, err)
		}
		if !resp.Found || resp.Username != "cj" || string(resp.Secret) != "secret" {
			t.Fatalf("response = %+v", resp)
		}
	}
	if runner.calls != 2 {
		t.Fatalf("decrypt calls = %d, want 2", runner.calls)
	}
}

func TestExecutorBitwardenUsesRequestScopedSession(t *testing.T) {
	runner := &fakeBitwardenRunner{out: []byte(`{"name":"Existing Edge Item","login":{"username":"admin","password":"secret"}}`)}
	executor := NewExecutor()
	executor.RegisterBitwarden("bw-lab", BitwardenProviderConfig{Runner: runner})

	_, err := executor.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "get",
		Ref:      "Existing Edge Item",
	})
	if err == nil || !strings.Contains(err.Error(), ErrBitwardenNotAuthenticated) {
		t.Fatalf("unauthenticated error = %v, want %q", err, ErrBitwardenNotAuthenticated)
	}
	resp, err := executor.HandleProviderRequest(context.Background(), ProviderRequest{
		Provider: "bw-lab",
		Action:   "get",
		Ref:      "Existing Edge Item",
		Session:  "bw-session-token",
	})
	if err != nil {
		t.Fatalf("HandleProviderRequest: %v", err)
	}
	if !resp.Found || resp.Username != "admin" || string(resp.Secret) != "secret" || resp.Ref != "Existing Edge Item" {
		t.Fatalf("response = %+v", resp)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("bw calls = %d, want 1 authenticated call", len(runner.calls))
	}
	if got := strings.Join(runner.calls[0].env, " "); got != "BW_SESSION=bw-session-token" {
		t.Fatalf("env = %q", got)
	}
	if got := strings.Join(runner.calls[0].args, " "); got != "get item Existing Edge Item" {
		t.Fatalf("args = %q", got)
	}
}

func TestConfiguredExecutorUsesEachProviderAgeKeyCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sops"), []byte("#!/bin/sh\nprintf '{\"password\":\"%s\"}\\n' \"$SOPS_AGE_KEY_CMD\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SOPS_AGE_KEY_CMD", "/keys/inherited")
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"personal": {Type: config.CredentialProviderSOPSAge, File: "personal.sops", AgeKeyCommand: "/keys/personal"},
		"work":     {Type: config.CredentialProviderSOPSAge, Config: config.CredentialProviderDetailConfig{File: "work.sops", AgeKeyCommand: "/keys/work"}},
	}
	executor := NewConfiguredExecutor(cfg)
	for _, name := range []string{"personal", "work", "personal"} {
		response, err := executor.HandleProviderRequest(context.Background(), ProviderRequest{Provider: name, Action: "get_noninteractive", Ref: "password"})
		if err != nil || !response.Found || string(response.Secret) != "/keys/"+name {
			t.Fatalf("%s provider command not selected: %v", name, err)
		}
	}
}
