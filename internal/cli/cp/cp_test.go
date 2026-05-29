package cp

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	clireconnect "github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
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
	oldRunScp := runScp
	defer func() {
		resolveHostForConnect = oldResolve
		runScp = oldRunScp
	}()

	var gotQuery, gotUser string
	resolveHostForConnect = func(query, explicitUser string, cfg ...*config.Config) (*clireconnect.ResolvedHost, error) {
		gotQuery = query
		gotUser = explicitUser
		return &clireconnect.ResolvedHost{
			Hostname:  query,
			Username:  "resolved-user",
			HostEntry: &sshconfig.HostEntry{Host: query},
			Credential: &clireconnect.ResolvedCredential{
				Username: "resolved-user",
				Password: secret.NewFromString("secret"),
			},
		}, nil
	}
	var scpArgs []string
	var gotPassword string
	runScp = func(args []string, password *secret.Secret) error {
		scpArgs = append([]string(nil), args...)
		if password != nil {
			_ = password.UseString(func(s string) error {
				gotPassword = s
				return nil
			})
		}
		return nil
	}

	if err := runCp("edge01:/tmp/file", "./file", false, false, false, false); err != nil {
		t.Fatalf("runCp: %v", err)
	}
	if gotQuery != "edge01" || gotUser != "" {
		t.Fatalf("resolve query=%q user=%q", gotQuery, gotUser)
	}
	if strings.Join(scpArgs, " ") != "resolved-user@edge01:/tmp/file ./file" {
		t.Fatalf("scp args = %#v", scpArgs)
	}
	if gotPassword != "secret" {
		t.Fatalf("password = %q", gotPassword)
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
