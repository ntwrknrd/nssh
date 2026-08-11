//go:build unix

package connector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
)

type MuxCheckRequest struct {
	Hostname     string
	Username     string
	Port         int
	SSHOptions   config.SSHHostConfig
	SSHVerbosity int
	SSHArgs      []string
	Timeout      int
}

type MuxCheckExec func(context.Context, []string) error

type MuxStartRequest struct {
	Hostname     string
	Username     string
	Port         int
	SSHOptions   config.SSHHostConfig
	SSHVerbosity int
	SSHArgs      []string
	Timeout      int
	Env          []string
}

type MuxStartExec func(context.Context, []string, []string) error

func CheckMuxSession(ctx context.Context, req MuxCheckRequest, execFn MuxCheckExec) (hot bool, checked bool) {
	args, ok := BuildMuxCheckArgs(req)
	if !ok {
		return false, false
	}
	if execFn == nil {
		execFn = defaultMuxCheckExec
	}
	timer := StartTiming(TimingMuxCheck)
	err := execFn(ctx, args)
	timer.Emit()
	return err == nil, true
}

func BuildMuxCheckArgs(req MuxCheckRequest) ([]string, bool) {
	options, _ := splitSSHArgs(req.SSHArgs)
	pinnedOptions, options := SplitPinnedHostKeyOptions(options)
	args := ComposeSSHOptions(SSHOptionPlan{
		Enforced:     pinnedOptions,
		Runtime:      options,
		Resolved:     req.SSHOptions,
		SSHVerbosity: req.SSHVerbosity,
	})
	if controlPath := effectiveSSHOption(args, "ControlPath"); controlPath == "" || strings.EqualFold(controlPath, "none") {
		return nil, false
	}

	if req.Timeout > 0 && effectiveSSHOption(args, "ConnectTimeout") == "" {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", req.Timeout))
	}
	if req.Port != 0 && req.Port != 22 && effectiveSSHOption(args, "Port") == "" {
		args = append(args, "-p", fmt.Sprintf("%d", req.Port))
	}
	args = append(args, "-O", "check", muxTarget(req.Username, req.Hostname))
	return args, true
}

func defaultMuxCheckExec(ctx context.Context, args []string) error {
	return exec.CommandContext(ctx, "ssh", args...).Run()
}

func StartMuxSession(ctx context.Context, req MuxStartRequest, execFn MuxStartExec) error {
	args, ok := BuildMuxStartArgs(req)
	if !ok {
		return nil
	}
	if execFn == nil {
		execFn = defaultMuxStartExec
	}
	timer := StartTiming(TimingMuxStart)
	err := execFn(ctx, args, req.Env)
	timer.Emit()
	return err
}

func BuildMuxStartArgs(req MuxStartRequest) ([]string, bool) {
	options, _ := splitSSHArgs(req.SSHArgs)
	pinnedOptions, options := SplitPinnedHostKeyOptions(options)
	args := ComposeSSHOptions(SSHOptionPlan{
		Enforced:     pinnedOptions,
		Runtime:      options,
		Resolved:     req.SSHOptions,
		SSHVerbosity: req.SSHVerbosity,
	})
	if !persistentMuxEnabled(args) {
		return nil, false
	}

	if len(req.Env) > 0 {
		if effectiveSSHOption(args, "NumberOfPasswordPrompts") == "" {
			args = append(args, "-o", "NumberOfPasswordPrompts=1")
		}
	} else if effectiveSSHOption(args, "BatchMode") == "" {
		args = append(args, "-o", "BatchMode=yes")
	}
	if req.Timeout > 0 && effectiveSSHOption(args, "ConnectTimeout") == "" {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", req.Timeout))
	}
	if req.Port != 0 && req.Port != 22 && effectiveSSHOption(args, "Port") == "" {
		args = append(args, "-p", fmt.Sprintf("%d", req.Port))
	}
	args = append(args, "-M", "-N", "-f", muxTarget(req.Username, req.Hostname))
	return args, true
}

func persistentMuxEnabled(args []string) bool {
	controlPath := effectiveSSHOption(args, "ControlPath")
	if controlPath == "" || strings.EqualFold(controlPath, "none") {
		return false
	}
	controlMaster := effectiveSSHOption(args, "ControlMaster")
	if controlMaster == "" || strings.EqualFold(controlMaster, "no") {
		return false
	}
	controlPersist := effectiveSSHOption(args, "ControlPersist")
	if controlPersist == "" || strings.EqualFold(controlPersist, "no") || controlPersist == "0" {
		return false
	}
	return true
}

func defaultMuxStartExec(ctx context.Context, args []string, env []string) error {
	cmd := exec.CommandContext(ctx, "ssh", args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func muxTarget(username, hostname string) string {
	if username == "" {
		return hostname
	}
	return username + "@" + hostname
}
