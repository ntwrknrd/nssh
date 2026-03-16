package remoteexec

import (
	"strings"
	"testing"
)

func TestBuildSSHArgsUsesTargetAlias(t *testing.T) {
	info := &HostInfo{
		Target:   "nre-netlab01",
		Hostname: "nre-netlab01.custcbb.local",
		Username: "nre",
	}
	cmd := RemoteCommand{Argv: []string{"containerlab", "inspect", "--all", "--format", "json"}}

	args := buildSSHArgs(info, cmd)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-o BatchMode=yes") {
		t.Fatalf("missing BatchMode=yes: %v", args)
	}
	if !strings.Contains(joined, "-l nre") {
		t.Fatalf("missing username: %v", args)
	}
	if strings.Contains(joined, "nre-netlab01.custcbb.local 'containerlab'") {
		t.Fatalf("should use host alias instead of hostname when target is set: %v", args)
	}
	if !strings.Contains(joined, "nre-netlab01 'containerlab'") {
		t.Fatalf("expected alias target in ssh args: %v", args)
	}
}

func TestBuildSSHArgsFallsBackToHostname(t *testing.T) {
	info := &HostInfo{
		Hostname: "nre-netlab01.custcbb.local",
		Username: "nre",
	}
	cmd := RemoteCommand{Argv: []string{"true"}}

	args := buildSSHArgs(info, cmd)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "nre-netlab01.custcbb.local 'true'") {
		t.Fatalf("expected hostname fallback in ssh args: %v", args)
	}
}
