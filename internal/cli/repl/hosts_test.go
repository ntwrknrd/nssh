package repl

import (
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	core "github.com/ntwrknrd/nssh/internal/repl"
)

func TestSplitHostList(t *testing.T) {
	got, err := splitHostList(" edge-a, edge-b ,2001:db8::1, [::1] ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "edge-a|edge-b|2001:db8::1|[::1]" {
		t.Fatalf("hosts=%q", got)
	}
	for _, input := range []string{"edge,,other", ",edge", "edge,", "edge,(other)", "edge,ssh://other", "edge,user@other", "edge,-other", "edge,space host", "edge,é"} {
		if _, err := splitHostList(input); err == nil {
			t.Errorf("%q accepted", input)
		}
	}
	if _, err := splitHostList(strings.Repeat("edge,", core.MaxSubmissionTargets) + "other"); err == nil {
		t.Fatal("too many targets accepted")
	}
}

func TestHostListModes(t *testing.T) {
	for _, args := range [][]string{
		{"-L", "8080:x:80"}, {"-qN"}, {"-NT"}, {"-qWother:22"}, {"-tt"}, {"-s"}, {"-S", "/tmp/shared"},
		{"-o", "ForkAfterAuthentication=yes"}, {"-o", "RequestTTY=force"}, {"-o", "RemoteCommand=echo"},
		{"-o", "SessionType=subsystem"}, {"-o", "Tunnel=yes"}, {"-o", "ControlMaster=yes"},
		{"-o", "ControlPath=/tmp/shared"}, {"-o", "LocalForward=8080 host:80"},
		{"-o", "DynamicForward=8080"}, {"-p0"}, {"-o", "ControlPath=/tmp/%C", "-S", "/tmp/shared"},
	} {
		if err := rejectHostListModes(args, config.SSHHostConfig{}); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
	for _, args := range [][]string{
		nil, {"-S", "/tmp/%C"}, {"-S", "none"}, {"-o", "ControlPath=/tmp/shared", "-S", "none"}, {"-T"}, {"-qJ", "jump1,jump2"}, {"-i", "-N"}, {"-o", "ProxyCommand=ssh -W %h:%p jump"},
		{"-o", "SetEnv=X=LocalForward=8080"}, {"-B", "lo", "-p2222", "-o", "ControlPath=none"},
	} {
		if err := rejectHostListModes(args, config.SSHHostConfig{}); err != nil {
			t.Errorf("rejected %q: %v", args, err)
		}
	}
}

func TestHostListValidatesResolvedProfiles(t *testing.T) {
	for name, value := range map[string]string{
		"RequestTTY": "force", "ForkAfterAuthentication": "yes", "RemoteForward": "8080 host:80",
		"ControlPath":   "/tmp/shared",
		"RemoteCommand": "echo", "SessionType": "none", "Tunnel": "point-to-point", "ControlMaster": "yes",
	} {
		policy := config.SSHHostConfig{Options: config.SSHOptions{name: config.NewSSHOptionString(value)}}
		if err := rejectHostListModes(nil, policy); err == nil {
			t.Errorf("accepted profile %s=%s", name, value)
		}
	}
	policy := config.SSHHostConfig{Options: config.SSHOptions{
		"RequestTTY":    config.NewSSHOptionString("force"),
		"ControlMaster": config.NewSSHOptionString("auto"),
		"ControlPath":   config.NewSSHOptionString("/tmp/%r@%h:%p"),
	}}
	for _, args := range [][]string{{"-T"}, {"-o", "RequestTTY=no"}} {
		if err := rejectHostListModes(args, policy); err != nil {
			t.Fatalf("effective override rejected: %v", err)
		}
	}
}

func TestHostListControlSocketScope(t *testing.T) {
	for _, tt := range []struct {
		path  string
		valid bool
	}{
		{"none", true}, {"/tmp/%C", true}, {"/tmp/%r@%h:%p", true},
		{"/tmp/shared", false}, {"/tmp/%%C", false}, {"/tmp/%h", false},
	} {
		policy := config.SSHHostConfig{Options: config.SSHOptions{"ControlPath": config.NewSSHOptionString(tt.path)}}
		if err := rejectHostListModes(nil, policy); (err == nil) != tt.valid {
			t.Errorf("%s: %v", tt.path, err)
		}
		if err := rejectHostListModes([]string{"-o", "ControlPath=none"}, policy); err != nil {
			t.Errorf("disabled %s: %v", tt.path, err)
		}
	}
}
