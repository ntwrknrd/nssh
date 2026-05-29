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

func TestOnePasswordGetHostCachesConfiguredSecretRefs(t *testing.T) {
	socketPath := fmt.Sprintf("/tmp/nssh-cred-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	restore := agent.SetSocketPathForTest(socketPath)
	defer restore()

	cancel, done := agent.RunInBackground(context.Background(), agent.NewCacheOnlyProvider(), agent.DefaultRuntimeConfig())
	defer func() {
		cancel()
		<-done
	}()
	waitForCredentialAgent(t)

	runner := &fakeOPRunner{outs: []fakeOPOut{
		{data: "netops"},
		{data: "secret"},
	}}
	provider := &onePasswordProvider{
		vault: "Network",
		hostRefs: map[string]config.CredentialRefConfig{
			"edge01": {
				Ref:         "op://Network/Edge 01/password",
				UsernameRef: "op://Network/Edge 01/username",
			},
		},
		runner: runner,
		cache:  true,
	}

	first, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("first GetHost: %v", err)
	}
	if first == nil || first.Username != "netops" || revealTestSecret(t, first) != "secret" {
		t.Fatalf("first record = %+v", first)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("first calls = %d, want 2", len(runner.calls))
	}

	second, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("second GetHost: %v", err)
	}
	if second == nil || second.Username != "netops" || revealTestSecret(t, second) != "secret" {
		t.Fatalf("second record = %+v", second)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("cached lookup made more op calls: %d", len(runner.calls))
	}
}

func TestOnePasswordGetHostCachesMissingDeterministicItem(t *testing.T) {
	socketPath := fmt.Sprintf("/tmp/nssh-cred-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	restore := agent.SetSocketPathForTest(socketPath)
	defer restore()

	cancel, done := agent.RunInBackground(context.Background(), agent.NewCacheOnlyProvider(), agent.DefaultRuntimeConfig())
	defer func() {
		cancel()
		<-done
	}()
	waitForCredentialAgent(t)

	runner := &fakeOPRunner{outs: []fakeOPOut{
		{data: "not found", err: errors.New("exit status 1")},
	}}
	provider := &onePasswordProvider{
		vault:  "Network",
		runner: runner,
		cache:  true,
	}

	first, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("first GetHost: %v", err)
	}
	if first != nil {
		t.Fatalf("first record = %+v, want nil", first)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("first calls = %d, want 1", len(runner.calls))
	}

	second, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("second GetHost: %v", err)
	}
	if second != nil {
		t.Fatalf("second record = %+v, want nil", second)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("cached miss made more op calls: %d", len(runner.calls))
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
	if err := record.Secret.UseString(func(s string) error {
		value = s
		return nil
	}); err != nil {
		t.Fatalf("secret: %v", err)
	}
	return value
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
