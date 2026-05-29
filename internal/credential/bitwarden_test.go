package credential

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
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

func TestBitwardenGetHostWithoutConfiguredRefReturnsNil(t *testing.T) {
	runner := &fakeBWRunner{}
	provider := &bitwardenProvider{runner: runner}

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
