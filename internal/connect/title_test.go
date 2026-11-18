package connect

import (
	"bytes"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func withTitleTerminal(t *testing.T, terminal bool) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	prevOutput, prevTerminal := titleOutput, titleTerminal
	titleOutput = &out
	titleTerminal = func() bool { return terminal }
	t.Cleanup(func() { titleOutput, titleTerminal = prevOutput, prevTerminal })
	return &out
}

func titleConfig(enabled bool) *config.Config {
	cfg := config.DefaultConfig()
	cfg.SSH.TerminalTitle = enabled
	return cfg
}

func TestSetTerminalTitlePushesSetsAndPops(t *testing.T) {
	out := withTitleTerminal(t, true)
	resolved := &ResolvedHost{Canonical: "acm-core1", Hostname: "10.0.0.1"}

	restore := setTerminalTitle(resolved, []string{"-l", "admin", "-p", "2222"}, titleConfig(true))
	if got, want := out.String(), "\x1b[22;2t\x1b]2;acm-core1\a"; got != want {
		t.Fatalf("set title = %q, want %q", got, want)
	}
	out.Reset()
	restore()
	if got, want := out.String(), "\x1b[23;2t"; got != want {
		t.Fatalf("restore = %q, want %q", got, want)
	}
}

func TestSetTerminalTitleGating(t *testing.T) {
	resolved := &ResolvedHost{Canonical: "acm-core1"}
	tests := []struct {
		name     string
		terminal bool
		cfg      *config.Config
		args     []string
	}{
		{name: "disabled", terminal: true, cfg: titleConfig(false)},
		{name: "nil config", terminal: true},
		{name: "not a terminal", terminal: false, cfg: titleConfig(true)},
		{name: "no command", terminal: true, cfg: titleConfig(true), args: []string{"-N", "-L", "8080:localhost:80"}},
		{name: "stdio forward", terminal: true, cfg: titleConfig(true), args: []string{"-W", "host:22"}},
		{name: "background", terminal: true, cfg: titleConfig(true), args: []string{"-fN"}},
		{name: "print config", terminal: true, cfg: titleConfig(true), args: []string{"-G"}},
		{name: "no tty", terminal: true, cfg: titleConfig(true), args: []string{"-T"}},
		{name: "subsystem", terminal: true, cfg: titleConfig(true), args: []string{"-s"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := withTitleTerminal(t, tt.terminal)
			setTerminalTitle(resolved, tt.args, tt.cfg)()
			if out.Len() != 0 {
				t.Fatalf("wrote %q, want nothing", out.String())
			}
		})
	}
}

func TestTerminalTitleValue(t *testing.T) {
	tests := []struct {
		name     string
		resolved *ResolvedHost
		want     string
	}{
		{name: "nil", want: ""},
		{name: "canonical", resolved: &ResolvedHost{Canonical: "acm-core1", Hostname: "acm-core1.example.com"}, want: "acm-core1"},
		{name: "hostname fallback", resolved: &ResolvedHost{Hostname: "192.0.2.10"}, want: "192.0.2.10"},
		{name: "strips controls", resolved: &ResolvedHost{Canonical: "bad\x1b]2;x\a\x9bna\u009bme\x7f"}, want: "bad]2;xname"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalTitle(tt.resolved); got != tt.want {
				t.Fatalf("terminalTitle = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetTerminalTitleSkipsEmptyTitle(t *testing.T) {
	out := withTitleTerminal(t, true)
	setTerminalTitle(&ResolvedHost{}, nil, titleConfig(true))()
	if out.Len() != 0 {
		t.Fatalf("wrote %q for empty title", out.String())
	}
}
