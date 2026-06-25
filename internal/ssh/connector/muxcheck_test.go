//go:build unix

package connector

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestBuildMuxCheckArgsUsesRenderedOptionsAndTarget(t *testing.T) {
	args, ok := BuildMuxCheckArgs(MuxCheckRequest{
		Hostname: "edge01.example.com",
		Username: "netops",
		Port:     2200,
		SSHOptions: config.SSHHostConfig{
			Options: config.SSHOptions{
				"ControlMaster": config.NewSSHOptionString("auto"),
				"ControlPath":   config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
			},
		},
		SSHArgs: []string{"-o", "LogLevel=ERROR"},
	})
	if !ok {
		t.Fatal("BuildMuxCheckArgs returned ok=false")
	}
	want := []string{
		"-F", "none",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=~/.ssh/sockets/%r@%h:%p",
		"-p", "2200",
		"-o", "LogLevel=ERROR",
		"-O", "check",
		"netops@edge01.example.com",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestBuildMuxCheckArgsSkipsWhenControlPathMissingOrNone(t *testing.T) {
	if args, ok := BuildMuxCheckArgs(MuxCheckRequest{Hostname: "edge01"}); ok || args != nil {
		t.Fatalf("missing ControlPath args=%#v ok=%v, want no check", args, ok)
	}

	args, ok := BuildMuxCheckArgs(MuxCheckRequest{
		Hostname: "edge01",
		SSHOptions: config.SSHHostConfig{Options: config.SSHOptions{
			"ControlPath": config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
		}},
		SSHArgs: []string{"-o", "ControlPath=none"},
	})
	if ok || args != nil {
		t.Fatalf("ControlPath none args=%#v ok=%v, want no check", args, ok)
	}
}

func TestCheckMuxSessionTreatsExitZeroAsHotAndErrorsAsCold(t *testing.T) {
	var gotArgs []string
	req := MuxCheckRequest{
		Hostname: "edge01",
		SSHOptions: config.SSHHostConfig{Options: config.SSHOptions{
			"ControlPath": config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
		}},
	}

	hot, checked := CheckMuxSession(context.Background(), req, func(_ context.Context, args []string) error {
		gotArgs = args
		return nil
	})
	if !checked || !hot {
		t.Fatalf("hot=%v checked=%v, want hot checked mux", hot, checked)
	}
	if len(gotArgs) == 0 {
		t.Fatal("mux check did not execute")
	}

	hot, checked = CheckMuxSession(context.Background(), req, func(_ context.Context, _ []string) error {
		return errors.New("no mux")
	})
	if !checked || hot {
		t.Fatalf("hot=%v checked=%v, want cold checked mux", hot, checked)
	}
}
