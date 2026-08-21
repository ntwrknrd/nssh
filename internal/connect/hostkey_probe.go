package connect

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
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

func probeInteractiveHostKey(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options, proxyEnv []string) hostKeyProbeStatus {
	args := buildHostKeyProbeArgs(resolved, sshArgs, cfg, opts)
	probeCtx := ctx
	cancel := func() {}
	if timeout := effectiveHostKeyProbeTimeout(args); timeout > 0 {
		probeCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	slog.Debug("probing host key", "host", resolved.Hostname, "argv", append([]string{"ssh"}, args...))
	cmd := exec.CommandContext(probeCtx, "ssh", args...)
	cmd.Env = append(withoutAskpassEnv(os.Environ()), proxyEnv...)
	output, _ := cmd.CombinedOutput()
	status := classifyHostKeyProbeOutput(output)
	slog.Debug("host key probe completed", "host", resolved.Hostname, "status", status.String())
	return status
}

func effectiveHostKeyProbeTimeout(args []string) time.Duration {
	value := connector.EffectiveSSHOption(args, "ConnectTimeout")
	if value == "" {
		return 10 * time.Second
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return 10 * time.Second
	}
	return time.Duration(seconds) * time.Second
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
	args := connector.ComposeSSHOptions(connector.SSHOptionPlan{
		Enforced: []string{
			"-o", "BatchMode=yes",
			"-o", "NumberOfPasswordPrompts=0",
			"-o", "KbdInteractiveAuthentication=no",
		},
		Runtime:      options,
		Resolved:     resolved.SSH,
		SSHVerbosity: opts.SSHVerbosity,
	})
	if cfg != nil && cfg.SSH.Connection.Timeout.Duration() > 0 && connector.EffectiveSSHOption(args, "ConnectTimeout") == "" {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", int(cfg.SSH.Connection.Timeout.Duration().Seconds())))
	}
	if resolved.Port != 0 && resolved.Port != 22 && connector.EffectiveSSHOption(args, "Port") == "" {
		args = append(args, "-p", fmt.Sprintf("%d", resolved.Port))
	}
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
			"NSSH_PROXY_SSH_ASKPASS", "NSSH_PROXY_ASKPASS_REQUIRE", "NSSH_PROXY_ASKPASS_SOCKET", "NSSH_PROXY_ASKPASS_NONCE":
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
