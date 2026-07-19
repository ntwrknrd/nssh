package connect

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

type hostKeyProbeStatus int

const (
	hostKeyProbeClean hostKeyProbeStatus = iota
	hostKeyProbeNeedsPrompt
	hostKeyProbeChanged
	hostKeyProbeInconclusive
)

func probeInteractiveHostKey(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options) hostKeyProbeStatus {
	args := buildHostKeyProbeArgs(resolved, sshArgs, cfg, opts)
	timeout := 10 * time.Second
	if cfg != nil && cfg.SSH.Connection.Timeout.Duration() > 0 {
		timeout = cfg.SSH.Connection.Timeout.Duration()
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	slog.Debug("probing host key", "host", resolved.Hostname, "argv", append([]string{"ssh"}, args...))
	cmd := exec.CommandContext(probeCtx, "ssh", args...)
	cmd.Env = withoutAskpassEnv(os.Environ())
	output, _ := cmd.CombinedOutput()
	status := classifyHostKeyProbeOutput(output)
	slog.Debug("host key probe completed", "host", resolved.Hostname, "status", status.String())
	return status
}

func (s hostKeyProbeStatus) String() string {
	switch s {
	case hostKeyProbeClean:
		return "clean"
	case hostKeyProbeNeedsPrompt:
		return "needs_host_key_prompt"
	case hostKeyProbeChanged:
		return "changed_host_key"
	default:
		return "inconclusive"
	}
}

func buildHostKeyProbeArgs(resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options) []string {
	options, _ := splitConnectSSHArgs(sshArgs)
	args := append([]string{}, connector.RenderSSHOptions(resolved.SSH, opts.SSHVerbosity)...)
	if cfg != nil && cfg.SSH.Connection.Timeout.Duration() > 0 && effectiveConnectSSHOption(append(args, options...), "ConnectTimeout") == "" {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", int(cfg.SSH.Connection.Timeout.Duration().Seconds())))
	}
	if resolved.Port != 0 && resolved.Port != 22 && explicitConnectSSHPort(options) == "" && effectiveConnectSSHOption(args, "Port") == "" {
		args = append(args, "-p", fmt.Sprintf("%d", resolved.Port))
	}
	args = append(args, options...)
	args = append(args,
		"-o", "BatchMode=yes",
		"-o", "NumberOfPasswordPrompts=0",
		"-o", "KbdInteractiveAuthentication=no",
	)
	args = append(args, connectTarget(resolved.Username, resolved.Hostname))
	return args
}

func classifyHostKeyProbeOutput(output []byte) hostKeyProbeStatus {
	lower := bytes.ToLower(output)
	switch {
	case bytes.Contains(lower, []byte("remote host identification has changed")):
		return hostKeyProbeChanged
	case bytes.Contains(lower, []byte("host key verification failed")):
		return hostKeyProbeNeedsPrompt
	case bytes.Contains(lower, []byte("the authenticity of host")):
		return hostKeyProbeNeedsPrompt
	case bytes.Contains(lower, []byte("are you sure you want to continue connecting")):
		return hostKeyProbeNeedsPrompt
	case bytes.Contains(lower, []byte("permission denied")):
		return hostKeyProbeClean
	case bytes.Contains(lower, []byte("no more authentication methods")):
		return hostKeyProbeClean
	case bytes.Contains(lower, []byte("authentications that can continue")):
		return hostKeyProbeClean
	default:
		return hostKeyProbeInconclusive
	}
}

func withoutAskpassEnv(env []string) []string {
	filtered := env[:0]
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		switch key {
		case "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "NSSH_ASKPASS_SOCKET", "NSSH_ASKPASS_NONCE",
			"NSSH_PROXY_SSH_ASKPASS", "NSSH_PROXY_ASKPASS_SOCKET", "NSSH_PROXY_ASKPASS_NONCE":
			continue
		default:
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func splitConnectSSHArgs(args []string) (options, command []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func connectTarget(username, hostname string) string {
	if username == "" {
		return hostname
	}
	return username + "@" + hostname
}

func explicitConnectSSHPort(args []string) string {
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
			key, value, ok := splitConnectOpenSSHOption(args[i+1])
			if ok && strings.EqualFold(key, "Port") {
				return value
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "-o") && len(arg) > 2 {
			key, value, ok := splitConnectOpenSSHOption(arg[2:])
			if ok && strings.EqualFold(key, "Port") {
				return value
			}
		}
	}
	return ""
}

func effectiveConnectSSHOption(args []string, want string) string {
	var found string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return found
		}
		if arg == "-o" && i+1 < len(args) {
			key, value, ok := splitConnectOpenSSHOption(args[i+1])
			if ok && strings.EqualFold(key, want) {
				found = value
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "-o") && len(arg) > 2 {
			key, value, ok := splitConnectOpenSSHOption(arg[2:])
			if ok && strings.EqualFold(key, want) {
				found = value
			}
		}
	}
	return found
}

func splitConnectOpenSSHOption(raw string) (string, string, bool) {
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
