package app

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func TestRootSSHGrammar(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		host             string
		options, command []string
		literal          bool
	}{
		{"simple", []string{"edge", "echo", "a,b"}, "edge", nil, []string{"echo", "a,b"}, false},
		{"split values", []string{"-B", "lo", "-e", "none", "-P", "tag,one", "edge", "echo"}, "edge", []string{"-B", "lo", "-e", "none", "-P", "tag,one"}, []string{"echo"}, false},
		{"attached", []string{"-Blo", "-enone", "-Ptag,one", "edge"}, "edge", []string{"-Blo", "-enone", "-Ptag,one"}, nil, false},
		{"clusters", []string{"-qp", "2222", "-qJjump1,jump2", "edge"}, "edge", []string{"-qp", "2222", "-qJjump1,jump2"}, nil, false},
		{"verbosity cluster", []string{"-vJ", "jump1,jump2", "edge"}, "edge", []string{"-J", "jump1,jump2"}, nil, false},
		{"opaque values", []string{"-i", "-lwrong", "-o", "ProxyCommand=printf a,b", "edge"}, "edge", []string{"-i", "-lwrong", "-o", "ProxyCommand=printf a,b"}, nil, false},
		{"post host options", []string{"edge", "-p", "2222", "echo", "-p", "42"}, "edge", []string{"-p", "2222"}, []string{"echo", "-p", "42"}, false},
		{"post host delimiter", []string{"edge", "--", "-p", "2222"}, "edge", nil, []string{"-p", "2222"}, false},
		{"pre host delimiter", []string{"--", "edge", "-p", "2222"}, "edge", nil, []string{"-p", "2222"}, false},
		{"literal command delimiter", []string{"edge", "--", "--", "a,b"}, "edge", nil, []string{"--", "a,b"}, false},
		{"command args preserved", []string{"edge", "printf", "", "a b", "a,b", "--", "-x"}, "edge", nil, []string{"printf", "", "a b", "a,b", "--", "-x"}, false},
		{"literal reserved", []string{"--target", "repl", "echo"}, "repl", nil, []string{"echo"}, true},
		{"literal equals", []string{"--target=log", "-p22", "echo"}, "log", []string{"-p22"}, []string{"echo"}, true},
		{"delimited reserved", []string{"--", "repl", "echo"}, "repl", nil, []string{"echo"}, false},
		{"comma remains one destination", []string{"a,b", "echo"}, "a,b", nil, []string{"echo"}, false},
		{"comma username", []string{"ops,team@edge", "echo"}, "edge", []string{"-l", "ops,team"}, []string{"echo"}, false},
		{"multiple at username", []string{"alice@a,bob@edge"}, "edge", []string{"-l", "alice@a,bob"}, nil, false},
		{"user before destination", []string{"-lfirst", "second@edge"}, "edge", []string{"-lfirst", "-l", "second"}, nil, false},
		{"user after destination", []string{"first@edge", "-lsecond"}, "edge", []string{"-l", "first", "-lsecond"}, nil, false},
		{"uri", []string{"ssh://ops@edge:2222", "echo"}, "edge", []string{"-l", "ops", "-p", "2222"}, []string{"echo"}, false},
		{"uri before options", []string{"ssh://ops@edge:2222", "-lother", "-p22", "echo"}, "edge", []string{"-l", "ops", "-p", "2222", "-lother", "-p22"}, []string{"echo"}, false},
		{"uri after options", []string{"-o", "User=first", "-p22", "ssh://ops@edge:2222"}, "edge", []string{"-o", "User=first", "-p22", "-l", "ops", "-p", "2222"}, nil, false},
		{"uri ipv6", []string{"ssh://ops@[2001:db8::1]:2222/"}, "2001:db8::1", []string{"-l", "ops", "-p", "2222"}, nil, false},
		{"plain ipv6", []string{"ops@[2001:db8::1]"}, "[2001:db8::1]", []string{"-l", "ops"}, nil, false},
		{"uri escaped user", []string{"ssh://ops%2Cteam@edge"}, "edge", []string{"-l", "ops,team"}, nil, false},
		{"uri plus decoding", []string{"ssh://ops+team@edge"}, "edge", []string{"-l", "ops team"}, nil, false},
		{"uri literal plus", []string{"ssh://ops%2Bteam@edge"}, "edge", []string{"-l", "ops+team"}, nil, false},
		{"uri connection parameters", []string{"ssh://ops;ignored@edge"}, "edge", []string{"-l", "ops"}, nil, false},
	}
	old := connectRequestFunc
	defer func() { connectRequestFunc = old }()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *connect.Request
			connectRequestFunc = func(_ context.Context, req connect.Request) error { got = &req; return nil }
			if err := execute(Options{}, tt.args); err != nil {
				t.Fatal(err)
			}
			if got == nil || got.Host != tt.host || got.LiteralTarget != tt.literal || !slices.Equal(got.SSHArgs, tt.options) || !slices.Equal(got.RemoteCommand, tt.command) {
				t.Fatalf("request=%+v, want host=%q options=%q command=%q literal=%v", got, tt.host, tt.options, tt.command, tt.literal)
			}
		})
	}
}

func TestRootSyntaxErrorsDoNotConnect(t *testing.T) {
	old := connectRequestFunc
	defer func() { connectRequestFunc = old }()
	connectRequestFunc = func(context.Context, connect.Request) error {
		t.Fatal("syntax error reached connection resolution")
		return nil
	}
	for _, args := range [][]string{
		{"-B"}, {"-vJ"}, {"edge", "-qp"}, {"--target"}, {"--target="}, {"-z", "edge"},
		{"ssh://"}, {"ssh://@edge"}, {"ssh://u%ZZ@edge"}, {"ssh://u@edge:bad"},
		{"ssh://u@edge:0"}, {"ssh://u@edge:65536"}, {"ssh://u@[::1"}, {"ssh://edge/path"},
	} {
		if err := execute(Options{}, args); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}

// OpenSSH itself is the oracle for destination/user/port precedence. -F none
// avoids reading user config, and -G never connects to the .invalid targets.
func TestRootParserMatchesOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH unavailable")
	}
	cases := [][]string{
		{"-B", "lo", "edge.invalid"}, {"-e", "none", "edge.invalid"},
		{"-qp", "2222", "edge.invalid"}, {"edge.invalid", "-p2222", "echo", "-p99"},
		{"-qJ", "jump1,jump2", "edge.invalid"}, {"-i", "-lbogus", "edge.invalid"},
		{"-lfirst", "second@edge.invalid"}, {"first@edge.invalid", "-lsecond"},
		{"-o", "User=first", "second@edge.invalid"}, {"first@edge.invalid", "-o", "User=second"},
		{"ssh://ops%2Cteam@edge.invalid:2222"},
		{"ssh://ops+team@edge.invalid"}, {"ssh://ops%2Bteam@edge.invalid"},
		{"ssh://ops@edge.invalid:2222/"},
		{"-o", `User="ops,team"`, "edge.invalid"}, {"-o", `Port="2222"`, "edge.invalid"},
		{"-p22", "ssh://ops@edge.invalid:2222"}, {"ssh://ops@edge.invalid:2222", "-p22"},
		{"-lfirst", "ssh://second@edge.invalid"}, {"ssh://first@edge.invalid", "-lsecond"},
		{"ops,team@edge.invalid"}, {"alice@a,bob@edge.invalid"},
		{"--", "edge.invalid", "-p2222"}, {"edge.invalid", "--", "-p2222"},
	}
	config := func(args []string) map[string]string {
		t.Helper()
		out, err := exec.Command(ssh, append([]string{"-G", "-F", "none"}, args...)...).Output()
		if err != nil {
			t.Fatalf("ssh -G %q: %v", args, err)
		}
		values := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			k, v, ok := strings.Cut(line, " ")
			if ok {
				values[k] = v
			}
		}
		return values
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			native := config(args)
			got, err := parseRootArgs(args)
			if err != nil {
				t.Fatal(err)
			}
			req := got.request
			normalized := append(slices.Clone(req.SSHArgs), req.Host, "--")
			normalized = append(normalized, req.RemoteCommand...)
			parsed := config(normalized)
			for _, key := range []string{"hostname", "user", "port", "proxyjump", "bindinterface", "escapechar"} {
				if parsed[key] != native[key] {
					t.Errorf("%s=%q, native=%q", key, parsed[key], native[key])
				}
			}
			for _, key := range []string{"User", "Port"} {
				if value := connector.EffectiveSSHOption(req.SSHArgs, key); value != "" && value != native[strings.ToLower(key)] {
					t.Errorf("resolver %s=%q, native=%q", key, value, native[strings.ToLower(key)])
				}
			}
		})
	}
}

func TestRootExplanationKeepsLegacyShortcutWithoutStealingSSHEscape(t *testing.T) {
	for _, flag := range []string{"--explain", "-e"} {
		got, err := parseRootArgs([]string{flag})
		if err != nil || got.request != nil || !slices.Equal(got.commandArgs, []string{"--explain"}) {
			t.Fatalf("explanation: %+v %v", got, err)
		}
	}
	got, err := parseRootArgs([]string{"-e", "none", "edge"})
	if err != nil || got.request == nil || !slices.Equal(got.request.SSHArgs, []string{"-e", "none"}) {
		t.Fatalf("escape: %+v %v", got, err)
	}
}
