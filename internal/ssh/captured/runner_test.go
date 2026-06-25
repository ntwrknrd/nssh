package captured

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func TestBuildOpenSSHArgsPreservesOptionsTargetAndCommand(t *testing.T) {
	req := Request{
		Hostname:      "edge01",
		Username:      "netops",
		Port:          2200,
		SSHArgs:       []string{"-o", "LogLevel=ERROR"},
		RemoteCommand: []string{"show", "version"},
		Timeout:       7 * time.Second,
	}

	got := buildOpenSSHArgs(req)

	want := []string{"-F", "none", "-o", "BatchMode=yes", "-o", "ConnectTimeout=7", "-p", "2200", "-o", "LogLevel=ERROR", "netops@edge01", "show", "version"}
	if !slices.Equal(got, want) {
		t.Fatalf("buildOpenSSHArgs() = %#v, want %#v", got, want)
	}
}

func TestBuildOpenSSHArgsRespectsExplicitPort(t *testing.T) {
	req := Request{
		Hostname:      "edge01",
		Port:          2200,
		SSHArgs:       []string{"-p", "2222"},
		RemoteCommand: []string{"show"},
	}

	got := buildOpenSSHArgs(req)

	want := []string{"-F", "none", "-o", "BatchMode=yes", "-p", "2222", "edge01", "show"}
	if !slices.Equal(got, want) {
		t.Fatalf("buildOpenSSHArgs() = %#v, want %#v", got, want)
	}
}

func TestBuildOpenSSHArgsRendersResolvedSSHOptions(t *testing.T) {
	req := Request{
		Hostname: "edge01",
		SSHOptions: config.SSHHostConfig{
			Options: config.SSHOptions{
				"ProxyCommand": config.NewSSHOptionString("ssh -F none -W %h:%p jump"),
			},
		},
		RemoteCommand: []string{"show"},
	}

	got := buildOpenSSHArgs(req)

	want := []string{"-F", "none", "-o", "ProxyCommand=ssh -F none -W %h:%p jump", "-o", "BatchMode=yes", "edge01", "show"}
	if !slices.Equal(got, want) {
		t.Fatalf("buildOpenSSHArgs() = %#v, want %#v", got, want)
	}
}

func TestBuildOpenSSHArgsPreservesSSHVerbosity(t *testing.T) {
	req := Request{
		Hostname:      "edge01",
		SSHVerbosity:  2,
		RemoteCommand: []string{"show"},
	}

	got := buildOpenSSHArgs(req)

	want := []string{"-F", "none", "-vv", "-o", "BatchMode=yes", "edge01", "show"}
	if !slices.Equal(got, want) {
		t.Fatalf("buildOpenSSHArgs() = %#v, want %#v", got, want)
	}
}

func TestBuildOpenSSHArgsAllowsAskpassPasswordPrompt(t *testing.T) {
	req := Request{
		Hostname:      "edge01",
		RemoteCommand: []string{"show"},
		Env:           []string{"SSH_ASKPASS=/tmp/nssh-askpass"},
	}

	got := buildOpenSSHArgs(req)

	want := []string{"-F", "none", "-o", "NumberOfPasswordPrompts=1", "edge01", "show"}
	if !slices.Equal(got, want) {
		t.Fatalf("buildOpenSSHArgs() = %#v, want %#v", got, want)
	}
}

func TestRunnerCapturesStdoutStderrAndExitCode(t *testing.T) {
	runner := Runner{
		Exec: func(_ context.Context, command Command) Result {
			if command.Name != "ssh" {
				t.Fatalf("command name = %q, want ssh", command.Name)
			}
			if !slices.Equal(command.Args, []string{"-F", "none", "-o", "BatchMode=yes", "edge01", "show"}) {
				t.Fatalf("command args = %#v", command.Args)
			}
			return Result{
				Stdout:   []byte("set protocols bgp\n"),
				Stderr:   []byte("warning\n"),
				ExitCode: 7,
				Err:      errors.New("exit status 7"),
			}
		},
	}

	result, err := runner.Run(context.Background(), Request{Hostname: "edge01", RemoteCommand: []string{"show"}})

	var exitErr *exit.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 7 {
		t.Fatalf("err = %v, want exit 7", err)
	}
	if !bytes.Equal(result.Stdout, []byte("set protocols bgp\n")) {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if !bytes.Equal(result.Stderr, []byte("warning\n")) {
		t.Fatalf("stderr = %q", result.Stderr)
	}
}

func TestRunnerEmitsSSHArgsBuildTiming(t *testing.T) {
	t.Setenv("NSSH_DEBUG", "1")

	stderr := captureStderr(t, func() {
		runner := Runner{
			Exec: func(_ context.Context, _ Command) Result {
				return Result{}
			},
		}
		_, err := runner.Run(context.Background(), Request{Hostname: "edge01", RemoteCommand: []string{"show"}})
		if err != nil {
			t.Fatalf("Runner.Run: %v", err)
		}
	})

	if !strings.Contains(stderr, "NSSH_TIMING:"+connector.TimingSSHArgsBuild+":") {
		t.Fatalf("stderr = %q, want ssh args build timing", stderr)
	}
}

func TestDefaultExecEmitsSSHProcessTimings(t *testing.T) {
	t.Setenv("NSSH_DEBUG", "1")

	stderr := captureStderr(t, func() {
		result := defaultExec(context.Background(), Command{
			Name: "sh",
			Args: []string{"-c", "printf ok"},
		})
		if result.Err != nil {
			t.Fatalf("defaultExec: %v", result.Err)
		}
	})

	for _, stage := range []string{
		connector.TimingSSHProcessStart,
		connector.TimingSSHProcessWait,
		connector.TimingSSHProcessTotal,
	} {
		if !strings.Contains(stderr, "NSSH_TIMING:"+stage+":") {
			t.Fatalf("stderr = %q, want %s timing", stderr, stage)
		}
	}
}

func TestDefaultExecRecordsOutputEventsInReadOrder(t *testing.T) {
	result := defaultExec(context.Background(), Command{
		Name: "sh",
		Args: []string{"-c", "printf 'banner\\n' >&2; sleep 0.1; printf 'set protocols bgp\\n'"},
	})
	if result.Err != nil {
		t.Fatalf("defaultExec: %v", result.Err)
	}
	if len(result.Output) != 2 {
		t.Fatalf("output events = %#v, want stderr then stdout", result.Output)
	}
	if result.Output[0].Stream != StreamStderr || string(result.Output[0].Data) != "banner\n" {
		t.Fatalf("first event = %#v, want stderr banner", result.Output[0])
	}
	if result.Output[1].Stream != StreamStdout || string(result.Output[1].Data) != "set protocols bgp\n" {
		t.Fatalf("second event = %#v, want stdout config", result.Output[1])
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	oldStderr := os.Stderr
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stderr = stderrWrite
	defer func() {
		os.Stderr = oldStderr
	}()

	fn()

	_ = stderrWrite.Close()
	stderr, err := io.ReadAll(stderrRead)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(stderr)
}
