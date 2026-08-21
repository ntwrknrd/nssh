//go:build unix

package connector

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
)

func TestExtractControlCommandSupportsSplitAndJoinedForms(t *testing.T) {
	command, rest, ok := ExtractControlCommand([]string{"-p", "2222", "-O", "exit"})
	if !ok || command != "exit" || !reflect.DeepEqual(rest, []string{"-p", "2222"}) {
		t.Fatalf("split command=%q rest=%#v ok=%v", command, rest, ok)
	}

	command, rest, ok = ExtractControlCommand([]string{"-Ocheck", "-o", "LogLevel=ERROR"})
	if !ok || command != "check" || !reflect.DeepEqual(rest, []string{"-o", "LogLevel=ERROR"}) {
		t.Fatalf("joined command=%q rest=%#v ok=%v", command, rest, ok)
	}
}

func TestBuildControlCommandArgsUsesRenderedOptionsAndTarget(t *testing.T) {
	args := BuildControlCommandArgs(ControlCommandRequest{
		Hostname: "edge01.example.com",
		Username: "netops",
		Port:     2200,
		Command:  "exit",
		SSHOptions: config.SSHHostConfig{Options: config.SSHOptions{
			"ControlMaster": config.NewSSHOptionString("auto"),
			"ControlPath":   config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
		}},
		SSHArgs: []string{"-o", "LogLevel=ERROR"},
	})

	want := []string{
		"-F", "none",
		"-o", "LogLevel=ERROR",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=~/.ssh/sockets/%r@%h:%p",
		"-p", "2200",
		"-O", "exit",
		"netops@edge01.example.com",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestBuildControlCommandArgsRuntimeOverridesResolvedOptions(t *testing.T) {
	args := BuildControlCommandArgs(ControlCommandRequest{
		Hostname: "edge01.example.com",
		Command:  "check",
		SSHOptions: config.SSHHostConfig{Options: config.SSHOptions{
			"ConnectTimeout": config.NewSSHOptionString("30"),
			"ControlPath":    config.NewSSHOptionString("/tmp/config.sock"),
		}},
		SSHArgs: []string{"-oConnectTimeout=60", "-S", "/tmp/runtime.sock"},
		Timeout: 15,
	})

	if got := EffectiveSSHOption(args, "ConnectTimeout"); got != "60" {
		t.Fatalf("effective ConnectTimeout = %q, want 60; args=%#v", got, args)
	}
	if got := EffectiveSSHOption(args, "ControlPath"); got != "/tmp/runtime.sock" {
		t.Fatalf("effective ControlPath = %q, want runtime path; args=%#v", got, args)
	}
}

func TestRunControlCommandReturnsOpenSSHExitWithoutGenericMessage(t *testing.T) {
	err := RunControlCommand(context.Background(), ControlCommandRequest{
		Hostname: "edge01",
		Command:  "exit",
	}, func(context.Context, []string) error {
		return &exec.ExitError{}
	})

	var exitErr *exit.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %T %v, want ExitError", err, err)
	}
	if exitErr.Code != 255 || exitErr.Message != "" {
		t.Fatalf("exit err = %+v, want code 255 with empty message", exitErr)
	}
}
