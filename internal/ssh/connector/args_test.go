//go:build unix

package connector

import (
	"slices"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestBuildSSHArgsPreservesOptionsTargetAndCommand(t *testing.T) {
	conn := NewConnector("edge01", "netops", nil, []string{"-p", "2222", "-o", "LogLevel=ERROR", "--", "show version"})
	conn.SetResolvedEndpoint("edge01", "2200")
	conn.SetTimeouts(&config.SSHConnectionConfig{Timeout: config.Duration(7 * time.Second)})

	got, err := conn.buildSSHArgs()
	if err != nil {
		t.Fatalf("buildSSHArgs() error = %v", err)
	}

	want := []string{"-F", "none", "-o", "ConnectTimeout=7", "-p", "2222", "-o", "LogLevel=ERROR", "netops@edge01", "show version"}
	if !slices.Equal(got, want) {
		t.Fatalf("buildSSHArgs() = %#v, want %#v", got, want)
	}
}

func TestBuildSSHArgsAddsDefaultTTYForInteractiveSession(t *testing.T) {
	conn := NewConnector("edge01", "netops", nil, []string{"-o", "LogLevel=ERROR"})
	conn.SetResolvedEndpoint("edge01", "2200")

	got, err := conn.buildSSHArgs()
	if err != nil {
		t.Fatalf("buildSSHArgs() error = %v", err)
	}

	want := []string{"-tt", "-F", "none", "-p", "2200", "-o", "LogLevel=ERROR", "netops@edge01"}
	if !slices.Equal(got, want) {
		t.Fatalf("buildSSHArgs() = %#v, want %#v", got, want)
	}
}

func TestBuildSSHArgsDoesNotAddDefaultTTYForRemoteCommand(t *testing.T) {
	conn := NewConnector("edge01", "netops", nil, []string{"--", "show", "version"})
	conn.SetResolvedEndpoint("edge01", "2200")

	got, err := conn.buildSSHArgs()
	if err != nil {
		t.Fatalf("buildSSHArgs() error = %v", err)
	}

	want := []string{"-F", "none", "-p", "2200", "netops@edge01", "show", "version"}
	if !slices.Equal(got, want) {
		t.Fatalf("buildSSHArgs() = %#v, want %#v", got, want)
	}
}

func TestBuildSSHArgsPreservesExplicitTTYFlagsForRemoteCommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "single force tty", args: []string{"-t", "--", "show"}, want: []string{"-F", "none", "-t", "edge01", "show"}},
		{name: "double force tty", args: []string{"-tt", "--", "show"}, want: []string{"-F", "none", "-tt", "edge01", "show"}},
		{name: "disable tty", args: []string{"-T", "--", "show"}, want: []string{"-F", "none", "-T", "edge01", "show"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewConnector("edge01", "", nil, tt.args)

			got, err := conn.buildSSHArgs()
			if err != nil {
				t.Fatalf("buildSSHArgs() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("buildSSHArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParsePortFromSSHArgsStopsAtRemoteCommand(t *testing.T) {
	conn := NewConnector("edge01", "", nil, []string{"-o", "Port=2200", "--", "-p", "9999"})
	if got := conn.parsePortFromSSHArgs(); got != "2200" {
		t.Fatalf("parsePortFromSSHArgs() = %q, want %q", got, "2200")
	}
}
