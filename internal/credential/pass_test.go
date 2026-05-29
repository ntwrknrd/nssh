package credential

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
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

func TestPassGetHostWithoutConfiguredRefReturnsNil(t *testing.T) {
	runner := &fakePassRunner{}
	provider := &passProvider{command: "pass", prefix: "nssh", runner: runner}

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
