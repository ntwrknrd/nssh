package connect

import (
	"slices"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestClassifyHostKeyProbeOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   hostKeyProbeStatus
	}{
		{
			name:   "known host auth failure is clean",
			output: "user@edge01: Permission denied (keyboard-interactive,password).",
			want:   hostKeyProbeClean,
		},
		{
			name:   "unknown host needs preparation",
			output: "The authenticity of host 'edge01' can't be established.\nAre you sure you want to continue connecting (yes/no/[fingerprint])?",
			want:   hostKeyProbeNeedsPrompt,
		},
		{
			name:   "changed host needs preparation",
			output: "@@@@@@@@@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @@@@@@@@@",
			want:   hostKeyProbeChanged,
		},
		{
			name:   "verification failure needs preparation",
			output: "Host key verification failed.",
			want:   hostKeyProbeNeedsPrompt,
		},
		{
			name:   "transport failure is inconclusive",
			output: "ssh: connect to host edge01 port 22: Connection refused",
			want:   hostKeyProbeInconclusive,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyHostKeyProbeOutput([]byte(tt.output)); got != tt.want {
				t.Fatalf("classifyHostKeyProbeOutput() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyHostKeyProbeOutputDistinguishesChangedHost(t *testing.T) {
	got := classifyHostKeyProbeOutput([]byte("@@@@@@@@@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @@@@@@@@@"))
	if got != hostKeyProbeChanged {
		t.Fatalf("changed host key status = %v, want %v", got, hostKeyProbeChanged)
	}
}

func TestBuildHostKeyProbeArgsDisablesPromptsAndUsesRenderedTarget(t *testing.T) {
	resolved := &ResolvedHost{
		Hostname: "edge01.example.net",
		Username: "netops",
		Port:     2200,
	}
	cfg := &config.Config{}
	cfg.SSH.Connection.Timeout = config.Duration(7 * time.Second)

	got := buildHostKeyProbeArgs(resolved, []string{"-o", "ControlPath=/tmp/nssh.sock", "--", "show version"}, cfg, Options{SSHVerbosity: 2})

	for _, want := range []string{"-F", "none", "-vv", "-o", "BatchMode=yes", "-o", "NumberOfPasswordPrompts=0", "-o", "KbdInteractiveAuthentication=no", "-o", "ConnectTimeout=7", "-p", "2200", "-o", "ControlPath=/tmp/nssh.sock", "netops@edge01.example.net"} {
		if !slices.Contains(got, want) {
			t.Fatalf("probe args missing %q: %#v", want, got)
		}
	}
	if slices.Contains(got, "show version") {
		t.Fatalf("probe args should ignore remote command: %#v", got)
	}
}

func TestWithoutAskpassEnvRemovesAskpassVariables(t *testing.T) {
	got := withoutAskpassEnv([]string{
		"PATH=/bin",
		"SSH_ASKPASS=/tmp/helper",
		"SSH_ASKPASS_REQUIRE=force",
		"NSSH_ASKPASS_SOCKET=/tmp/sock",
		"NSSH_ASKPASS_NONCE=secret",
	})
	want := []string{"PATH=/bin"}
	if !slices.Equal(got, want) {
		t.Fatalf("withoutAskpassEnv() = %#v, want %#v", got, want)
	}
}
