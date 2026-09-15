package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

type sshSyntaxFixture struct{ dir, binary string }

func newSSHSyntaxFixture(t *testing.T, binary string) *sshSyntaxFixture {
	t.Helper()
	f := &sshSyntaxFixture{dir: t.TempDir(), binary: binary}
	for _, dir := range []string{"bin", "home", "config/nssh", "data", "state"} {
		if err := os.MkdirAll(filepath.Join(f.dir, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.dir, "config", "nssh", "config.yaml"), []byte("ssh:\n  defaults: {}\ninventory:\n  providers:\n    local:\n      type: local\n      hosts:\n        node:\n          auth:\n            mode: key\n        edge.example:\n          auth:\n            mode: key\n        repl:\n          auth:\n            mode: key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '\036' >> "$NSSH_TEST_ARGV"
for arg do printf '%s\037' "$arg" >> "$NSSH_TEST_ARGV"; done
exit 0
`
	if err := os.WriteFile(filepath.Join(f.dir, "bin", "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *sshSyntaxFixture) run(t *testing.T, args ...string) [][]string {
	t.Helper()
	log := filepath.Join(f.dir, "argv")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.binary, args...)
	cmd.Env = []string{"PATH=" + filepath.Join(f.dir, "bin") + ":/usr/bin:/bin", "HOME=" + filepath.Join(f.dir, "home"), "XDG_CONFIG_HOME=" + filepath.Join(f.dir, "config"), "XDG_DATA_HOME=" + filepath.Join(f.dir, "data"), "XDG_STATE_HOME=" + filepath.Join(f.dir, "state"), "NSSH_TEST_ARGV=" + log}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nssh %q: %v\n%s", args, err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("fake ssh log: %v", err)
	}
	var calls [][]string
	for _, call := range bytes.Split(data, []byte{0x1e}) {
		if len(call) == 0 {
			continue
		}
		parts := bytes.Split(call, []byte{0x1f})
		// The final delimiter is not an argument. Earlier empty fields are.
		values := make([]string, 0, len(parts)-1)
		for _, part := range parts[:len(parts)-1] {
			values = append(values, string(part))
		}
		calls = append(calls, values)
	}
	return calls
}
func requireTail(t *testing.T, argv []string, want ...string) {
	t.Helper()
	if len(argv) < len(want) || !bytes.Equal([]byte(strings.Join(argv[len(argv)-len(want):], "\x00")), []byte(strings.Join(want, "\x00"))) {
		t.Fatalf("argv tail=%q, want %q", argv, want)
	}
}
func requireSequence(t *testing.T, argv []string, want ...string) {
	t.Helper()
	at := 0
	for _, token := range want {
		for at < len(argv) && argv[at] != token {
			at++
		}
		if at == len(argv) {
			t.Fatalf("argv=%q missing ordered token %q", argv, token)
		}
		at++
	}
}
func TestExecutableSSHSyntaxPreservesRootGrammar(t *testing.T) {
	binary := buildBinary(t)
	f := newSSHSyntaxFixture(t, binary)
	cases := []struct {
		name       string
		args, want []string
	}{
		{"split B e P", []string{"-B", "0.0.0.0", "-e", "none", "-P", "tag,blue", "--target", "node", "--", "show", "version"}, []string{"-B", "0.0.0.0", "-e", "none", "-P", "tag,blue", "--", "node", "show", "version"}},
		{"clusters and comma option", []string{"-vJ", "jump.example", "-qp", "2202", "-o", "ProxyJump=a,b", "--target", "node", "--", "show"}, []string{"-J", "jump.example", "-qp", "2202", "-o", "ProxyJump=a,b", "--", "node", "show"}},
		{"post host options", []string{"node", "-p", "2203", "--", "-dash-command"}, []string{"-p", "2203", "--", "node", "-dash-command"}},
		{"post host separator", []string{"node", "--", "-dash-command"}, []string{"--", "node", "-dash-command"}},
		{"separator before host", []string{"--", "node", "-dash-command"}, []string{"--", "node", "-dash-command"}},
		{"option-looking identity file", []string{"-i", "-identity-file", "--target", "node", "--", "show"}, []string{"-i", "-identity-file", "--", "node", "show"}},
		{"reserved literal", []string{"--target", "repl", "--", "show"}, []string{"--", "repl", "show"}},
		{"dash-looking literal destination", []string{"--target", "-oProxyCommand=unexpected", "--", "show"}, []string{"--", "-oProxyCommand=unexpected", "show"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := f.run(t, tc.args...)
			if len(calls) == 0 {
				t.Fatal("fake ssh was not invoked")
			}
			argv := calls[len(calls)-1]
			requireSequence(t, argv, tc.want...)
			requireTail(t, argv, tc.want[len(tc.want)-3:]...)
		})
	}
}
func TestExecutableSSHURIIdentityAndOptionPrecedence(t *testing.T) {
	f := newSSHSyntaxFixture(t, buildBinary(t))
	for _, tc := range []struct {
		name             string
		args, want       []string
		port, userOption string
	}{
		{"URI supplies identity and port", []string{"--target", "ssh://uriuser@edge.example:2204", "--", "show"}, []string{"-p", "2204", "--", "uriuser@edge.example", "show"}, "2204", "uriuser"},
		{"options before URI win", []string{"-l", "override", "-p", "2205", "--target", "ssh://uriuser@edge.example:2204", "--", "show"}, []string{"-l", "override", "-p", "2205", "--", "override@edge.example", "show"}, "2205", "override"},
		{"URI before post-host options wins", []string{"ssh://uriuser@edge.example:2204", "-l", "later", "-p", "2205", "--", "show"}, []string{"-p", "2204", "--", "uriuser@edge.example", "show"}, "2204", "uriuser"},
		{"URI IPv6", []string{"--target", "ssh://uriuser@[2001:db8::1]:2204", "--", "show"}, []string{"-p", "2204", "--", "uriuser@2001:db8::1", "show"}, "2204", "uriuser"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := f.run(t, tc.args...)
			if len(calls) == 0 {
				t.Fatal("fake ssh was not invoked")
			}
			argv := calls[len(calls)-1]
			requireSequence(t, argv, tc.want...)
			requireTail(t, argv, tc.want[len(tc.want)-3:]...)
			if port := connector.EffectiveSSHOption(argv, "Port"); port != tc.port {
				t.Fatalf("effective port=%q argv=%q", port, argv)
			}
			if user := connector.EffectiveSSHOption(argv, "User"); user != tc.userOption {
				t.Fatalf("effective user=%q argv=%q", user, argv)
			}
		})
	}
}
