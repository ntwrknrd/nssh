package cp

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	clireconnect "github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/secret"
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
	resolvedPassword := secret.NewFromString("secret")
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
		}, nil
	}
	var scpArgs []string
	var gotPassword string
	runScp = func(args []string, password *secret.Secret) error {
		scpArgs = append([]string(nil), args...)
		if password != nil {
			_ = password.Use(func(pw []byte) error {
				gotPassword = string(pw)
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
	if strings.Join(scpArgs, " ") != "-F none -o Compression=yes -P 2200 resolved-user@edge01.example.com:/tmp/file ./file" {
		t.Fatalf("scp args = %#v", scpArgs)
	}
	if gotPassword != "secret" {
		t.Fatalf("password = %q", gotPassword)
	}
	if err := resolvedPassword.Use(func([]byte) error { return nil }); err == nil {
		t.Fatal("resolved password was not destroyed after cp")
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
