package credential

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/secret"
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

func TestOnePasswordGetHostReadsDeterministicItem(t *testing.T) {
	runner := &fakeOPRunner{outs: []fakeOPOut{{
		data: `{"title":"nssh host edge01","fields":[{"label":"username","value":"admin"},{"label":"password","value":"secret"}]}`,
	}}}
	provider := &onePasswordProvider{account: "ntwrknrd", vault: "Network", runner: runner}

	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	var gotSecret string
	if got != nil && got.Secret != nil {
		if err := got.Secret.UseString(func(s string) error {
			gotSecret = s
			return nil
		}); err != nil {
			t.Fatalf("secret: %v", err)
		}
	}
	if got == nil || got.Username != "admin" || gotSecret != "secret" {
		t.Fatalf("record = %+v", got)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d", len(runner.calls))
	}
	wantArgs := []string{"item", "get", "nssh host edge01", "--vault", "Network", "--account", "ntwrknrd", "--format", "json", "--reveal"}
	if strings.Join(runner.calls[0].args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %#v", runner.calls[0].args)
	}
}

func TestOnePasswordSetHostCreatesDeterministicItemWhenMissing(t *testing.T) {
	runner := &fakeOPRunner{outs: []fakeOPOut{
		{data: "not found", err: errors.New("exit status 1")},
		{},
	}}
	provider := &onePasswordProvider{vault: "Network", runner: runner}

	err := provider.SetHost("edge01", &Record{Username: "admin", Secret: secret.NewFromString("secret")})
	if err != nil {
		t.Fatalf("SetHost: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d", len(runner.calls))
	}
	if strings.Join(runner.calls[1].args, " ") != "item create --vault Network -" {
		t.Fatalf("create args = %#v", runner.calls[1].args)
	}
	for _, want := range []string{`"title":"nssh host edge01"`, `"label":"username"`, `"value":"admin"`, `"label":"password"`, `"value":"secret"`} {
		if !strings.Contains(runner.calls[1].stdin, want) {
			t.Fatalf("missing %s in stdin: %s", want, runner.calls[1].stdin)
		}
	}
}

func TestOnePasswordSetGroupEditsExistingItem(t *testing.T) {
	runner := &fakeOPRunner{outs: []fakeOPOut{
		{data: `{"id":"abc","title":"nssh group lab","fields":[{"label":"username","value":"old"},{"label":"password","value":"oldpass"}]}`},
		{},
	}}
	provider := &onePasswordProvider{vault: "Network", runner: runner}

	err := provider.SetGroup("lab", &Record{Username: "netops", Secret: secret.NewFromString("newpass")})
	if err != nil {
		t.Fatalf("SetGroup: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d", len(runner.calls))
	}
	if strings.Join(runner.calls[1].args, " ") != "item edit nssh group lab --vault Network -" {
		t.Fatalf("edit args = %#v", runner.calls[1].args)
	}
	for _, want := range []string{`"title":"nssh group lab"`, `"value":"netops"`, `"value":"newpass"`} {
		if !strings.Contains(runner.calls[1].stdin, want) {
			t.Fatalf("missing %s in stdin: %s", want, runner.calls[1].stdin)
		}
	}
}
