package connect

import (
	"context"
	"errors"
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

func (f *fakeRexOpener) CanSelect(context.Context) error {
	if f.canSelect {
		return nil
	}
	return errors.New("refused")
}

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
	t.Setenv("NSSH_REX_OPEN", "")
	t.Setenv("NSSH_RECORDING_INNER", "")
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
		inner    bool
	}{
		{name: "nil config", opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a"}},
		{name: "disabled", cfg: disabled, opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a"}},
		{name: "single_host here", cfg: here, opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a"}},
		{name: "not a terminal", opener: &fakeRexOpener{canSelect: true}, targets: []string{"a"}},
		{name: "ssh options", opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a"}, sshArgs: []string{"-p", "2222"}},
		{name: "launched pane", opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a", "b"}, inPane: true},
		{name: "recording child", opener: &fakeRexOpener{canSelect: true}, terminal: true, targets: []string{"a", "b"}, inner: true},
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
			if tt.inner {
				t.Setenv("NSSH_RECORDING_INNER", "1")
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

func withSelectHosts(t *testing.T, pick func(options []string) []string) *[]string {
	t.Helper()
	var offered []string
	prev := selectHostsFunc
	selectHostsFunc = func(_ string, options []string, _ string) ([]string, error) {
		offered = options
		return pick(options), nil
	}
	t.Cleanup(func() { selectHostsFunc = prev })
	return &offered
}

func TestRexPickHostsOpensSeveral(t *testing.T) {
	opener := &fakeRexOpener{}
	withRexOpener(t, opener)
	withTitleTerminal(t, true)
	offered := withSelectHosts(t, func(options []string) []string { return options })
	query, handled, err := rexPickHosts(t.Context(), "ops@dfz-core", "", nil, testSmartResolveConfig())
	if err != nil || !handled || query != "ops@dfz-core" {
		t.Fatalf("query=%q handled=%v err=%v", query, handled, err)
	}
	if len(*offered) != 2 || len(opener.hosts) != 2 || opener.hosts[0].Name != "ops@clab-dfz-core01" {
		t.Fatalf("offered=%q hosts=%+v", *offered, opener.hosts)
	}
}

func TestRexPickHostsSinglePickConnects(t *testing.T) {
	opener := &fakeRexOpener{}
	withRexOpener(t, opener)
	withTitleTerminal(t, true)
	withSelectHosts(t, func(options []string) []string { return options[1:] })
	query, handled, err := rexPickHosts(t.Context(), "dfz-core", "", nil, testSmartResolveConfig())
	if err != nil || handled || query != "clab-dfz-core02" || opener.hosts != nil {
		t.Fatalf("query=%q handled=%v err=%v hosts=%v", query, handled, err, opener.hosts)
	}
}

func TestRexPickHostsCanceled(t *testing.T) {
	withRexOpener(t, &fakeRexOpener{})
	withTitleTerminal(t, true)
	withSelectHosts(t, func([]string) []string { return nil })
	if _, _, err := rexPickHosts(t.Context(), "dfz-core", "", nil, testSmartResolveConfig()); err == nil {
		t.Fatal("canceled selection did not error")
	}
}

func TestRexPickHostsDefersToNormalLookup(t *testing.T) {
	cfg := testSmartResolveConfig()
	off := false
	disabled := testSmartResolveConfig()
	disabled.Rex.Enabled = &off
	tests := []struct {
		name     string
		query    string
		cfg      *config.Config
		sshArgs  []string
		terminal bool
		opener   *fakeRexOpener
	}{
		{name: "exact host", query: "dfz-core01", cfg: cfg, terminal: true, opener: &fakeRexOpener{}},
		{name: "one match", query: "cacti", cfg: cfg, terminal: true, opener: &fakeRexOpener{}},
		{name: "ssh options", query: "dfz-core", cfg: cfg, sshArgs: []string{"-p", "22"}, terminal: true, opener: &fakeRexOpener{}},
		{name: "no terminal", query: "dfz-core", cfg: cfg, opener: &fakeRexOpener{}},
		{name: "disabled", query: "dfz-core", cfg: disabled, terminal: true, opener: &fakeRexOpener{}},
		{name: "no rex client", query: "dfz-core", cfg: cfg, terminal: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withRexOpener(t, tt.opener)
			withTitleTerminal(t, tt.terminal)
			withSelectHosts(t, func([]string) []string {
				t.Fatal("multi-select opened")
				return nil
			})
			query, handled, err := rexPickHosts(t.Context(), tt.query, "", tt.sshArgs, tt.cfg)
			if err != nil || handled || query != tt.query {
				t.Fatalf("query=%q handled=%v err=%v", query, handled, err)
			}
		})
	}
}

func TestOpenInRexPaletteForcesSingleHost(t *testing.T) {
	opener := &fakeRexOpener{}
	withRexOpener(t, opener)
	withTitleTerminal(t, false)
	if OpenInRex(t.Context(), config.DefaultConfig(), []string{"acm-core1"}, nil) {
		t.Fatal("opened a single host without a terminal")
	}
	t.Setenv("NSSH_REX_OPEN", "1")
	if !OpenInRex(t.Context(), config.DefaultConfig(), []string{"acm-core1"}, nil) || len(opener.hosts) != 1 {
		t.Fatalf("palette open declined: %+v", opener.hosts)
	}
}
