package credential

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

type fakeBWRunner struct {
	calls []fakeBWCall
	outs  []fakeBWOut
}

type fakeBWCall struct {
	args  []string
	stdin string
}

type fakeBWOut struct {
	data string
	err  error
}

func (f *fakeBWRunner) Run(_ context.Context, stdin []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, fakeBWCall{args: append([]string(nil), args...), stdin: string(stdin)})
	if len(f.outs) == 0 {
		return nil, nil
	}
	out := f.outs[0]
	f.outs = f.outs[1:]
	return []byte(out.data), out.err
}

func TestBitwardenGetHostReadsDeterministicItem(t *testing.T) {
	runner := &fakeBWRunner{outs: []fakeBWOut{{
		data: `{"name":"nssh host edge01","login":{"username":"admin","password":"secret"}}`,
	}}}
	provider := &bitwardenProvider{runner: runner}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Username != "admin" || revealTestSecret(t, got) != "secret" || got.Ref != "nssh host edge01" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if strings.Join(runner.calls[0].args, " ") != "get item nssh host edge01" {
		t.Fatalf("args = %#v", runner.calls[0].args)
	}
}

func TestBitwardenGetGroupReadsDeterministicItem(t *testing.T) {
	runner := &fakeBWRunner{outs: []fakeBWOut{{
		data: `{"name":"nssh group custcbb","login":{"username":"netops","password":"secret"}}`,
	}}}
	provider := &bitwardenProvider{runner: runner}

	got, err := provider.GetGroup("custcbb")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if got == nil || got.Username != "netops" || revealTestSecret(t, got) != "secret" || got.Ref != "nssh group custcbb" {
		t.Fatalf("record = %+v secret=%q", got, revealTestSecret(t, got))
	}
	if strings.Join(runner.calls[0].args, " ") != "get item nssh group custcbb" {
		t.Fatalf("args = %#v", runner.calls[0].args)
	}
}

func TestBitwardenGetHostReadsConfiguredRef(t *testing.T) {
	runner := &fakeBWRunner{outs: []fakeBWOut{{
		data: `{"name":"Existing Edge Item","login":{"username":"admin","password":"secret"}}`,
	}}}
	provider := &bitwardenProvider{
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {Ref: "Existing Edge Item"},
		},
		runner: runner,
	}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got == nil || got.Ref != "Existing Edge Item" {
		t.Fatalf("record = %+v", got)
	}
	if strings.Join(runner.calls[0].args, " ") != "get item Existing Edge Item" {
		t.Fatalf("args = %#v", runner.calls[0].args)
	}
}

func TestBitwardenSetHostCreatesEncodedLoginItem(t *testing.T) {
	runner := &fakeBWRunner{outs: []fakeBWOut{
		{data: "not found", err: errors.New("exit status 1")},
		{data: "encoded-json"},
		{},
	}}
	provider := &bitwardenProvider{runner: runner}

	err := provider.SetHost("edge01", &Record{Username: "admin", Secret: secret.NewFromString("secret")})
	if err != nil {
		t.Fatalf("SetHost: %v", err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls = %d", len(runner.calls))
	}
	if strings.Join(runner.calls[1].args, " ") != "encode" {
		t.Fatalf("encode args = %#v", runner.calls[1].args)
	}
	for _, want := range []string{`"name":"nssh host edge01"`, `"username":"admin"`, `"password":"secret"`} {
		if !strings.Contains(runner.calls[1].stdin, want) {
			t.Fatalf("missing %s in encoded stdin: %s", want, runner.calls[1].stdin)
		}
	}
	if strings.Join(runner.calls[2].args, " ") != "create item encoded-json" {
		t.Fatalf("create args = %#v", runner.calls[2].args)
	}
}

func TestBitwardenMissingItemReturnsNil(t *testing.T) {
	runner := &fakeBWRunner{outs: []fakeBWOut{{
		data: "Not found.",
		err:  errors.New("exit status 1"),
	}}}
	provider := &bitwardenProvider{runner: runner}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if got != nil {
		t.Fatalf("record = %+v, want nil", got)
	}
}
