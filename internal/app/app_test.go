package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestRunSuppressesEmptyExitErrorMessage(t *testing.T) {
	oldConnectRequest := connectRequestFunc
	defer func() { connectRequestFunc = oldConnectRequest }()
	connectRequestFunc = func(context.Context, connect.Request) error {
		return &exit.ExitError{Code: 255}
	}

	oldStderr := os.Stderr
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stderr = stderrWrite
	code := Run(Options{Version: "test", Args: []string{"edge01"}})
	_ = stderrWrite.Close()
	os.Stderr = oldStderr
	data, err := io.ReadAll(stderrRead)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if code != 255 {
		t.Fatalf("exit code = %d, want 255", code)
	}
	if got := string(data); got != "" {
		t.Fatalf("stderr = %q, want no nssh wrapper", got)
	}
}

func TestRootCommandRegistersPublicCommands(t *testing.T) {
	root := NewRootCmd(Options{Version: "test"})

	var got []string
	for _, cmd := range root.Commands() {
		if !cmd.Hidden {
			got = append(got, cmd.Name())
		}
	}
	want := []string{"agent", "cp", "inv", "log", "repl", "self"}
	if !slices.Equal(got, want) {
		t.Fatalf("public commands = %v, want %v", got, want)
	}
}

func TestPublicCommandIntegration(t *testing.T) {
	root := NewRootCmd(Options{Version: "test"})
	var want []string
	for _, cmd := range root.Commands() {
		if cmd.Hidden {
			continue
		}
		want = append(want, cmd.Name())
		args := []string{cmd.Name(), "--help"}
		got, err := parseRootArgs(args)
		if err != nil || got.request != nil || !slices.Equal(got.commandArgs, args) {
			t.Errorf("public command %s routed incorrectly: %+v, %v", cmd.Name(), got, err)
		}
		if cmd.Long != "" && cmd.Flags().Lookup("explain") == nil {
			t.Errorf("public command %s has extended help but no --explain flag", cmd.Name())
		}
	}
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"__list-subcommands"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(out.String())
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("listed commands = %v, registered commands = %v", got, want)
	}
}

func TestSmartConnectPassesRemoteCommandAsParsedRequest(t *testing.T) {
	oldConnectRequest := connectRequestFunc
	defer func() { connectRequestFunc = oldConnectRequest }()

	var got connect.Request
	connectRequestFunc = func(_ context.Context, req connect.Request) error {
		got = req
		return nil
	}

	err := execute(Options{Version: "test"}, []string{"edge01", "show", "version"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got.Host != "edge01" {
		t.Fatalf("host = %q, want edge01", got.Host)
	}
	if got.LiteralTarget {
		t.Fatal("literal target should be false")
	}
	if len(got.SSHArgs) != 0 {
		t.Fatalf("ssh args = %#v, want none", got.SSHArgs)
	}
	if len(got.RemoteCommand) != 2 || got.RemoteCommand[0] != "show" || got.RemoteCommand[1] != "version" {
		t.Fatalf("remote command = %#v", got.RemoteCommand)
	}
}

func TestSmartConnectPreservesLiteralTargetAndSSHArgsInParsedRequest(t *testing.T) {
	oldConnectRequest := connectRequestFunc
	defer func() { connectRequestFunc = oldConnectRequest }()

	var got connect.Request
	connectRequestFunc = func(_ context.Context, req connect.Request) error {
		got = req
		return nil
	}

	err := execute(Options{Version: "test"}, []string{"-p", "2222", "--target", "log", "show", "version"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got.Host != "log" {
		t.Fatalf("host = %q, want log", got.Host)
	}
	if !got.LiteralTarget {
		t.Fatal("literal target should be true")
	}
	if len(got.SSHArgs) != 2 || got.SSHArgs[0] != "-p" || got.SSHArgs[1] != "2222" {
		t.Fatalf("ssh args = %#v", got.SSHArgs)
	}
	if len(got.RemoteCommand) != 2 || got.RemoteCommand[0] != "show" || got.RemoteCommand[1] != "version" {
		t.Fatalf("remote command = %#v", got.RemoteCommand)
	}
}

func TestAgentRestartRejectedByRootCommand(t *testing.T) {
	err := execute(Options{Version: "test"}, []string{"agent", "restart"})
	if err == nil {
		t.Fatal("agent restart succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), `unknown command "restart"`) {
		t.Fatalf("agent restart error = %v, want unknown command", err)
	}
}

func TestLogArchiveCommandIsRegisteredWithoutOpportunisticFlag(t *testing.T) {
	root := NewRootCmd(Options{Version: "test"})
	cmd, _, err := root.Find([]string{"log", "archive"})
	if err != nil {
		t.Fatalf("find log archive: %v", err)
	}
	if cmd.Name() != "archive" {
		t.Fatalf("log archive command name = %q", cmd.Name())
	}
	if flag := cmd.Flags().Lookup("opportunistic"); flag != nil {
		t.Fatal("log archive must not expose --opportunistic")
	}
}

func TestSelfHelpDoesNotTruncate(t *testing.T) {
	root := NewRootCmd(Options{Version: "test"})
	for _, path := range [][]string{
		{"self"},
		{"self", "reinstall"},
	} {
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Fatalf("find %v: %v", path, err)
		}
		help := ui.RenderStyledHelp(cmd, ui.StyledHelpConfig{ShowGlobalFlags: true, Width: 80})
		if strings.Contains(help, "...") {
			t.Fatalf("%s help should not be truncated:\n%s", strings.Join(path, " "), help)
		}
	}
}

func TestSelfBenchRegistersOnlyConnectionBenchmarks(t *testing.T) {
	root := NewRootCmd(Options{Version: "test"})
	cmd, _, err := root.Find([]string{"self", "bench"})
	if err != nil {
		t.Fatalf("find self bench: %v", err)
	}

	var got []string
	for _, subcmd := range cmd.Commands() {
		if !subcmd.Hidden {
			got = append(got, subcmd.Name())
		}
	}
	want := []string{"scp", "ssh"}
	if !slices.Equal(got, want) {
		t.Fatalf("self bench commands = %v, want %v", got, want)
	}
}
