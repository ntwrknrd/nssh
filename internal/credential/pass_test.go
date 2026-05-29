package credential

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

type fakePassRunner struct {
	calls   []fakePassCall
	outs    []fakePassOut
	missing bool
}

type fakePassCall struct {
	args  []string
	stdin string
}

type fakePassOut struct {
	data string
	err  error
}

func (f *fakePassRunner) Run(_ context.Context, stdin []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, fakePassCall{args: append([]string(nil), args...), stdin: string(stdin)})
	if len(f.outs) == 0 {
		return nil, nil
	}
	out := f.outs[0]
	f.outs = f.outs[1:]
	return []byte(out.data), out.err
}

func (f *fakePassRunner) LookPath(name string) (string, error) {
	if f.missing {
		return "", errors.New("not found")
	}
	return "/usr/bin/" + name, nil
}

func TestPassGetHostReadsDeterministicEntry(t *testing.T) {
	runner := &fakePassRunner{outs: []fakePassOut{{
		data: "secret\nusername: admin\nignored: value\n",
	}}}
	provider := &passProvider{command: "pass", prefix: "nssh", runner: runner}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "admin" || revealTestSecret(t, got) != "secret" || got.Ref != "nssh/hosts/edge01" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d", len(runner.calls))
	}
	if strings.Join(runner.calls[0].args, " ") != "show nssh/hosts/edge01" {
		t.Fatalf("args = %#v", runner.calls[0].args)
	}
}

func TestPassGetGroupReadsDeterministicEntry(t *testing.T) {
	runner := &fakePassRunner{outs: []fakePassOut{{
		data: "secret\nusername: netops\n",
	}}}
	provider := &passProvider{command: "pass", prefix: "nssh", runner: runner}

	got, err := provider.GetGroup("custcbb")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if got == nil || got.Username != "netops" || revealTestSecret(t, got) != "secret" || got.Ref != "nssh/groups/custcbb" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if strings.Join(runner.calls[0].args, " ") != "show nssh/groups/custcbb" {
		t.Fatalf("args = %#v", runner.calls[0].args)
	}
}

func TestPassGetHostReadsConfiguredRef(t *testing.T) {
	runner := &fakePassRunner{outs: []fakePassOut{{
		data: "secret\nusername: admin\n",
	}}}
	provider := &passProvider{
		command: "pass",
		prefix:  "nssh",
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {Ref: "custom/edge01"},
		},
		runner: runner,
	}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Ref != "custom/edge01" {
		t.Fatalf("record = %+v", got)
	}
	if strings.Join(runner.calls[0].args, " ") != "show custom/edge01" {
		t.Fatalf("args = %#v", runner.calls[0].args)
	}
}

func TestPassSetHostWritesExpectedEntry(t *testing.T) {
	runner := &fakePassRunner{}
	provider := &passProvider{command: "pass", prefix: "nssh", runner: runner}

	err := provider.SetHost("edge01", &Record{Username: "admin", Secret: secret.NewFromString("secret")})
	if err != nil {
		t.Fatalf("SetHost: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d", len(runner.calls))
	}
	if strings.Join(runner.calls[0].args, " ") != "insert --multiline --force nssh/hosts/edge01" {
		t.Fatalf("args = %#v", runner.calls[0].args)
	}
	if runner.calls[0].stdin != "secret\nusername: admin\n" {
		t.Fatalf("stdin = %q", runner.calls[0].stdin)
	}
}

func TestPassMissingEntryReturnsNil(t *testing.T) {
	runner := &fakePassRunner{outs: []fakePassOut{{
		data: "Error: nssh/hosts/edge01 is not in the password store.",
		err:  errors.New("exit status 1"),
	}}}
	provider := &passProvider{command: "pass", prefix: "nssh", runner: runner}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got != nil {
		t.Fatalf("record = %+v, want nil", got)
	}
}

func TestPassStatusUnavailableWhenCommandMissing(t *testing.T) {
	provider := &passProvider{command: "pass", prefix: "nssh", runner: &fakePassRunner{missing: true}}

	status := provider.Status()
	if status.Type != config.CredentialProviderPass {
		t.Fatalf("status type = %q", status.Type)
	}
	if status.Available {
		t.Fatalf("status should be unavailable: %+v", status)
	}
}
