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
	rendered := RenderSSHOptions(req.SSHOptions, req.SSHVerbosity)
	allOptions := append([]string{}, rendered...)
	allOptions = append(allOptions, options...)
	if controlPath := effectiveSSHOption(allOptions, "ControlPath"); controlPath == "" || strings.EqualFold(controlPath, "none") {
		return nil, false
	}

	args := append([]string{}, rendered...)
	if req.Timeout > 0 && effectiveSSHOption(allOptions, "ConnectTimeout") == "" {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", req.Timeout))
	}
	if req.Port != 0 && req.Port != 22 && explicitSSHPort(options) == "" && effectiveSSHOption(rendered, "Port") == "" {
		args = append(args, "-p", fmt.Sprintf("%d", req.Port))
	}
	args = append(args, options...)
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
	rendered := RenderSSHOptions(req.SSHOptions, req.SSHVerbosity)
	allOptions := append([]string{}, rendered...)
	allOptions = append(allOptions, options...)
	if !persistentMuxEnabled(allOptions) {
		return nil, false
	}

	args := append([]string{}, rendered...)
	if len(req.Env) > 0 {
		if effectiveSSHOption(allOptions, "NumberOfPasswordPrompts") == "" {
			args = append(args, "-o", "NumberOfPasswordPrompts=1")
		}
	} else if effectiveSSHOption(allOptions, "BatchMode") == "" {
		args = append(args, "-o", "BatchMode=yes")
	}
	if req.Timeout > 0 && effectiveSSHOption(allOptions, "ConnectTimeout") == "" {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", req.Timeout))
	}
	if req.Port != 0 && req.Port != 22 && explicitSSHPort(options) == "" && effectiveSSHOption(rendered, "Port") == "" {
		args = append(args, "-p", fmt.Sprintf("%d", req.Port))
	}
	args = append(args, options...)
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

func explicitSSHPort(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return ""
		}
		if arg == "-p" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "-p") && len(arg) > 2 {
			return arg[2:]
		}
		if arg == "-o" && i+1 < len(args) {
			key, value, ok := splitOpenSSHOption(args[i+1])
			if ok && strings.EqualFold(key, "Port") {
				return value
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "-o") && len(arg) > 2 {
			key, value, ok := splitOpenSSHOption(arg[2:])
			if ok && strings.EqualFold(key, "Port") {
				return value
			}
		}
	}
	return ""
}

func effectiveSSHOption(args []string, want string) string {
	var found string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return found
		}
		if arg == "-o" && i+1 < len(args) {
			key, value, ok := splitOpenSSHOption(args[i+1])
			if ok && strings.EqualFold(key, want) {
				found = value
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "-o") && len(arg) > 2 {
			key, value, ok := splitOpenSSHOption(arg[2:])
			if ok && strings.EqualFold(key, want) {
				found = value
			}
		}
	}
	return found
}

func splitOpenSSHOption(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	if key, value, ok := strings.Cut(raw, "="); ok {
		return strings.TrimSpace(key), strings.TrimSpace(value), true
	}
	fields := strings.Fields(raw)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], strings.Join(fields[1:], " "), true
}
