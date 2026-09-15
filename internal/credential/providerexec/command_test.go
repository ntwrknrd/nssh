package providerexec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/ntwrknrd/nssh/internal/credential/sopsdoc"
)

func TestCommandRequestDoesNotSignIn(t *testing.T) {
	runner := &fakeOnePasswordRunner{errs: []error{errors.New("not signed in")}}
	e := NewExecutor()
	e.Register1Password("op", OnePasswordProviderConfig{Runner: runner})
	_, err := e.HandleProviderRequest(context.Background(), ProviderRequest{Provider: "op", Action: "get", Ref: "op://vault/item/password", Username: "admin", NonInteractive: true})
	if err == nil || !strings.Contains(err.Error(), "authentication before") {
		t.Fatalf("err=%v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("unexpected signin attempts: %v", runner.calls)
	}
}

func TestCommandSOPSAgeTerminalIsolation(t *testing.T) {
	if os.Getenv("NSSH_SOPS_TTY_HELPER") == "1" {
		runSOPSTerminalHelper(t)
		return
	}

	dir := t.TempDir()
	logPath := filepath.Join(dir, "tty.log")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCommandSOPSAgeTerminalIsolation$")
	cmd.Env = append(os.Environ(), "NSSH_SOPS_TTY_HELPER=1", "NSSH_SOPS_TTY_LOG="+logPath, "NSSH_SOPS_TTY_DIR="+dir)
	terminal, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&output, terminal)
		close(readDone)
	}()
	err = cmd.Wait()
	_ = terminal.Close()
	<-readDone
	if err != nil {
		t.Fatalf("helper: %v\n%s", err, output.String())
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Fields(string(data)), []string{"tty", "no-tty", "tty"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("terminal access = %q, want %q", got, want)
	}
}

func runSOPSTerminalHelper(t *testing.T) {
	t.Helper()
	dir := os.Getenv("NSSH_SOPS_TTY_DIR")
	logPath := os.Getenv("NSSH_SOPS_TTY_LOG")
	if dir == "" || logPath == "" {
		t.Fatal("missing SOPS PTY helper environment")
	}
	script := filepath.Join(dir, "sops")
	const body = "#!/bin/sh\nif ( : < /dev/tty ) 2>/dev/null; then\n  printf 'tty\\n' >> \"$NSSH_SOPS_TTY_LOG\"\nelse\n  printf 'no-tty\\n' >> \"$NSSH_SOPS_TTY_LOG\"\nfi\nprintf '%s\\n' '{\"secret\":\"value\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor()
	executor.RegisterSOPSAge("sops", SOPSAgeProviderConfig{File: "fixture.sops", Runner: sopsdoc.CLIRunner{Command: script}})
	for _, request := range []ProviderRequest{
		{Provider: "sops", Action: "get", Ref: "secret"},
		{Provider: "sops", Action: "get_noninteractive", Ref: "secret"},
		{Provider: "sops", Action: "get", Ref: "secret"},
	} {
		response, err := executor.HandleProviderRequest(context.Background(), request)
		if err != nil || !response.Found || string(response.Secret) != "value" {
			t.Fatalf("request %#v: response=%+v err=%v", request, response, err)
		}
	}
}
