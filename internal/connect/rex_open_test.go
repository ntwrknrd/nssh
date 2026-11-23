package connect

import (
	"context"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/rex"
)

type fakeRexOpener struct {
	canSelect bool
	err       error
	placement rex.Placement
	hosts     []rex.Host
}

func (f *fakeRexOpener) CanSelect(context.Context) bool { return f.canSelect }

func (f *fakeRexOpener) Open(_ context.Context, p rex.Placement, hosts []rex.Host) (rex.Opened, error) {
	f.placement, f.hosts = p, hosts
	return rex.Opened{Session: "nssh", Tabs: []string{"x"}, Selected: true}, f.err
}

func withRexOpener(t *testing.T, opener *fakeRexOpener) {
	t.Helper()
	prevClient, prevExe := rexClientFunc, rexExecutableFunc
	rexClientFunc = func() rexOpener {
		if opener == nil {
			return nil
		}
		return opener
	}
	rexExecutableFunc = func() (string, error) { return "/bin/nssh", nil }
	t.Cleanup(func() { rexClientFunc, rexExecutableFunc = prevClient, prevExe })
	t.Setenv("NSSH_REX_PANE", "")
}

func TestOpenInRexSingleHost(t *testing.T) {
	opener := &fakeRexOpener{canSelect: true}
	withRexOpener(t, opener)
	withTitleTerminal(t, true)
	cfg := config.DefaultConfig()
	cfg.Rex.PanesPerTab = 4
	if !OpenInRex(t.Context(), cfg, []string{"acm-core1"}, nil) {
		t.Fatal("did not open in Rex")
	}
	if opener.placement != (rex.Placement{Session: "nssh", PanesPerTab: 4, Split: "auto"}) {
		t.Fatalf("placement = %+v", opener.placement)
	}
	if len(opener.hosts) != 1 || opener.hosts[0].Name != "acm-core1" || opener.hosts[0].Argv[len(opener.hosts[0].Argv)-1] != "acm-core1" {
		t.Fatalf("hosts = %+v", opener.hosts)
	}
}

func TestOpenInRexFallsBack(t *testing.T) {
	disabled := config.DefaultConfig()
	off := false
	disabled.Rex.Enabled = &off
	here := config.DefaultConfig()
	here.Rex.SingleHost = config.RexSingleHostHere
	tests := []struct {
		name     string
		cfg      *config.Config
		opener   *fakeRexOpener
		terminal bool
		targets  []string
		sshArgs  []string
		inPane   bool
	}{
		{name: "nil config", opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a"}},
		{name: "disabled", cfg: disabled, opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a"}},
		{name: "single_host here", cfg: here, opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a"}},
		{name: "not a terminal", opener: &fakeRexOpener{canSelect: true}, targets: []string{"a"}},
		{name: "ssh options", opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a"}, sshArgs: []string{"-p", "2222"}},
		{name: "launched pane", opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a", "b"}, inPane: true},
		{name: "no rex", terminal: true, targets: []string{"a", "b"}},
		{name: "remote control off", opener: &fakeRexOpener{}, terminal: true, targets: []string{"a"}},
		{name: "open fails", opener: &fakeRexOpener{canSelect: true, err: context.DeadlineExceeded}, terminal: true, targets: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withRexOpener(t, tt.opener)
			withTitleTerminal(t, tt.terminal)
			if tt.inPane {
				t.Setenv("NSSH_REX_PANE", "1")
			}
			cfg := tt.cfg
			if cfg == nil && tt.name != "nil config" {
				cfg = config.DefaultConfig()
			}
			if OpenInRex(t.Context(), cfg, tt.targets, tt.sshArgs) {
				t.Fatal("opened in Rex, want fallback")
			}
		})
	}
}

func TestOpenInRexMultiHostNeedsNoTerminalOrSelect(t *testing.T) {
	opener := &fakeRexOpener{}
	withRexOpener(t, opener)
	withTitleTerminal(t, false)
	if !OpenInRex(t.Context(), config.DefaultConfig(), []string{"a", "b"}, nil) || len(opener.hosts) != 2 {
		t.Fatalf("multi-host open declined: %+v", opener.hosts)
	}
}

func TestRexTarget(t *testing.T) {
	if got := rexTarget("core", "", &ResolvedHost{Canonical: "acm-core1"}); got != "acm-core1" {
		t.Fatalf("canonical = %q", got)
	}
	if got := rexTarget("admin@core", "admin", &ResolvedHost{Canonical: "acm-core1"}); got != "admin@acm-core1" {
		t.Fatalf("explicit user = %q", got)
	}
	if got := rexTarget("10.0.0.1", "", &ResolvedHost{Hostname: "10.0.0.1"}); got != "10.0.0.1" {
		t.Fatalf("ad hoc = %q", got)
	}
}
