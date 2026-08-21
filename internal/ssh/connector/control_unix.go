//go:build unix

package connector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
)

type ControlCommandRequest struct {
	Hostname     string
	Username     string
	Port         int
	Command      string
	SSHOptions   config.SSHHostConfig
	SSHVerbosity int
	SSHArgs      []string
	Timeout      int
}

type ControlCommandExec func(context.Context, []string) error

func ExtractControlCommand(args []string) (command string, rest []string, ok bool) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i:]...)
			return "", rest, false
		}
		if arg == "-O" {
			if i+1 >= len(args) {
				rest = append(rest, arg)
				return "", rest, false
			}
			rest = append(rest, args[i+2:]...)
			return args[i+1], rest, true
		}
		if strings.HasPrefix(arg, "-O") && len(arg) > 2 {
			rest = append(rest, args[i+1:]...)
			return arg[2:], rest, true
		}
		rest = append(rest, arg)
	}
	return "", rest, false
}

func RunControlCommand(ctx context.Context, req ControlCommandRequest, execFn ControlCommandExec) error {
	if execFn == nil {
		execFn = defaultControlCommandExec
	}
	err := execFn(ctx, BuildControlCommandArgs(req))
	if err == nil {
		return nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		code := exitErr.ExitCode()
		if code < 0 {
			code = 255
		}
		return &exit.ExitError{Code: code, Cause: err}
	}
	return fmt.Errorf("ssh control command: %w", err)
}

func BuildControlCommandArgs(req ControlCommandRequest) []string {
	options, _ := splitSSHArgs(req.SSHArgs)
	pinnedOptions, options := SplitPinnedHostKeyOptions(options)
	args := ComposeSSHOptions(SSHOptionPlan{
		Enforced:     pinnedOptions,
		Runtime:      options,
		Resolved:     req.SSHOptions,
		SSHVerbosity: req.SSHVerbosity,
	})
	if req.Timeout > 0 && effectiveSSHOption(args, "ConnectTimeout") == "" {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", req.Timeout))
	}
	if req.Port != 0 && req.Port != 22 && effectiveSSHOption(args, "Port") == "" {
		args = append(args, "-p", fmt.Sprintf("%d", req.Port))
	}
	args = append(args, "-O", req.Command, muxTarget(req.Username, req.Hostname))
	return args
}

func defaultControlCommandExec(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
