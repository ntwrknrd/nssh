package cp

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	clireconnect "github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func TestBareCpPrintsHelp(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		cmd := NewCmd()
		cmd.SetArgs([]string{})
		err = cmd.Execute()
	})

	if err != nil {
		t.Fatalf("bare cp should show help, got error: %v", err)
	}
	for _, want := range []string{
		"cp <source> <dest>",
		"Copy files via SCP",
		"--recursive",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help output missing %q:\n%s", want, out)
		}
	}
}

func TestRunCpUsesSharedResolvePath(t *testing.T) {
	oldResolve := resolveHostForConnect
	oldStartAskpass := startResolvedAskpass
	oldPrepareHostKey := prepareResolvedHostKey
	oldRunScp := runScp
	defer func() {
		resolveHostForConnect = oldResolve
		startResolvedAskpass = oldStartAskpass
		prepareResolvedHostKey = oldPrepareHostKey
		runScp = oldRunScp
	}()

	var gotQuery, gotUser string
	resolvedPassword := secret.NewFromString("secret")
	proxyPassword := secret.NewFromString("proxy-secret")
	resolveHostForConnect = func(query, explicitUser string, cfg ...*config.Config) (*clireconnect.ResolvedHost, error) {
		gotQuery = query
		gotUser = explicitUser
		return &clireconnect.ResolvedHost{
			Hostname: "edge01.example.com",
			Port:     2200,
			Username: "resolved-user",
			SSH:      config.SSHHostConfig{Options: config.SSHOptions{"Compression": config.NewSSHOptionBool(true)}},
			Credential: &clireconnect.ResolvedCredential{
				Username: "resolved-user",
				Password: resolvedPassword,
			},
			Proxy: &clireconnect.ResolvedProxy{
				Credential: &clireconnect.ResolvedCredential{Password: proxyPassword},
			},
		}, nil
	}
	startResolvedAskpass = func(context.Context, *clireconnect.ResolvedHost) (*clireconnect.AskpassEnvironment, error) {
		return &clireconnect.AskpassEnvironment{
			Env:      []string{"SSH_ASKPASS=/tmp/nssh-askpass"},
			ProxyEnv: []string{"NSSH_PROXY_SSH_ASKPASS=/tmp/nssh-askpass"},
		}, nil
	}
	var gotProxyEnv []string
	prepareResolvedHostKey = func(_ context.Context, _ *clireconnect.ResolvedHost, _ []string, proxyEnv []string) (*connector.HostKeyPreparation, error) {
		gotProxyEnv = append([]string(nil), proxyEnv...)
		return &connector.HostKeyPreparation{TempKnownHosts: "/tmp/nssh-test-known-hosts"}, nil
	}
	var scpArgs []string
	var gotEnv []string
	runScp = func(args, env []string) error {
		scpArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return nil
	}

	if err := runCp(context.Background(), "edge01:/tmp/file", "./file", false, false, false, false); err != nil {
		t.Fatalf("runCp: %v", err)
	}
	if gotQuery != "edge01" || gotUser != "" {
		t.Fatalf("resolve query=%q user=%q", gotQuery, gotUser)
	}
	if strings.Join(scpArgs, " ") != "-F none -o Compression=yes -P 2200 -o UserKnownHostsFile=/tmp/nssh-test-known-hosts -o StrictHostKeyChecking=yes resolved-user@edge01.example.com:/tmp/file ./file" {
		t.Fatalf("scp args = %#v", scpArgs)
	}
	if strings.Join(gotEnv, " ") != "SSH_ASKPASS=/tmp/nssh-askpass" {
		t.Fatalf("scp env = %#v", gotEnv)
	}
	if strings.Join(gotProxyEnv, " ") != "NSSH_PROXY_SSH_ASKPASS=/tmp/nssh-askpass" {
		t.Fatalf("proxy env = %#v", gotProxyEnv)
	}
	if err := resolvedPassword.Use(func([]byte) error { return nil }); err == nil {
		t.Fatal("resolved password was not destroyed after cp")
	}
	if err := proxyPassword.Use(func([]byte) error { return nil }); err == nil {
		t.Fatal("proxy password was not destroyed after cp")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = original })

	fn()

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
