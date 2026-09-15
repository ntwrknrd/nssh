package repl

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/exit"
	core "github.com/ntwrknrd/nssh/internal/repl"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/sshargs"
)

// RunHosts executes a root host list using the shared scheduler and capture path.
// Callers identify bare list candidates before normalizing user@host or URIs.
// An exact whole-token alias retains the ordinary single-target path.
func RunHosts(ctx context.Context, req connect.Request) (handled bool, err error) {
	if req.LiteralTarget || !strings.Contains(req.Host, ",") {
		return false, nil
	}
	cfg, err := config.LoadDefault()
	if err != nil {
		return true, err
	}
	cat, err := connect.BuildHostCatalog(cfg)
	if err != nil {
		return true, err
	}
	if _, ok := cat.Find(req.Host); ok {
		return false, nil
	}

	size := len(req.Host)
	for _, args := range [][]string{req.SSHArgs, req.RemoteCommand} {
		for _, arg := range args {
			size += len(arg)
		}
	}
	if size > core.MaxSubmissionBytes {
		return true, fmt.Errorf("host-list invocation exceeds %d bytes", core.MaxSubmissionBytes)
	}
	label := strings.Join(req.RemoteCommand, " ")
	if len(req.RemoteCommand) == 0 {
		return true, fmt.Errorf("comma-separated hosts require a remote command")
	}
	parts, err := splitHostList(req.Host)
	if err != nil {
		return true, err
	}

	restore := secret.ManageInterrupts()
	defer restore()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	explicitUser := connector.EffectiveSSHOption(req.SSHArgs, "User")
	// Executor resolves every target before starting workers. This closure only
	// inspects catalog metadata; provider lookup happens once per actual job.
	resolve := func(_ context.Context, target core.Target) ([]core.ResolvedTarget, error) {
		identity, labelUser := target.Value, explicitUser
		policy := cfg.SSH.Defaults
		if data, ok := cat.Find(target.Value); ok {
			identity, policy = data.Canonical, data.SSH
			if labelUser == "" {
				labelUser = data.Username
			}
		}
		if err := rejectHostListModes(req.SSHArgs, policy); err != nil {
			return nil, fmt.Errorf("%s: %w", target.Value, err)
		}
		if labelUser != "" {
			identity = labelUser + "@" + identity
		}
		return []core.ResolvedTarget{{Identity: identity, Value: target.Value}}, nil
	}
	run := func(runCtx context.Context, target core.ResolvedTarget, _ []string) core.Result {
		resolved, err := connect.ResolveLiteralHostFromCatalog(runCtx, target.Value.(string), explicitUser, cfg, cat)
		if err != nil {
			return core.Result{Err: err, ExitCode: 1}
		}
		// Commands in core.Submission are display labels here. The original
		// argv is passed intact so one root command stays one command per host.
		result, err := connect.RunRemoteCommandCapture(runCtx, resolved, req.RemoteCommand, connect.CaptureOptions{
			Options: req.Options, SSHArgs: req.SSHArgs, MaxOutputBytes: core.MaxCommandOutput,
		})
		return core.Result{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode, Err: firstError(err, result.Err), Truncated: result.Truncated}
	}
	sub := core.Submission{Commands: []string{label}}
	for _, part := range parts {
		sub.Targets = append(sub.Targets, core.Target{Value: part})
	}
	output := hostListOutput{out: os.Stdout, errOut: os.Stderr}
	executor := core.Executor{Concurrency: core.DefaultConcurrency, Resolve: resolve, Run: run,
		OnTargets: func(targets []core.ResolvedTarget) { output.start(label, targets) },
		OnEvent:   output.event,
	}
	events, err := executor.Execute(ctx, sub)
	output.recap(events)
	if ctx.Err() != nil {
		return true, &exit.ExitError{Code: 130}
	}
	if err != nil {
		return true, err
	}
	for _, event := range events {
		if event.State == core.Failed {
			return true, &exit.ExitError{Code: 1, Message: "one or more remote commands failed"}
		}
	}
	return true, nil
}

func splitHostList(value string) ([]string, error) {
	if strings.Count(value, ",")+1 > core.MaxSubmissionTargets {
		return nil, fmt.Errorf("host list exceeds %d targets", core.MaxSubmissionTargets)
	}
	parts := strings.Split(value, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("host list contains an empty member")
		}
		address := part
		if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
			address = part[1 : len(part)-1]
		}
		if _, err := netip.ParseAddr(address); err != nil {
			for _, ch := range part {
				valid := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.'
				if !valid {
					return nil, fmt.Errorf("invalid bare host-list member %q", part)
				}
			}
			if strings.HasPrefix(part, "-") {
				return nil, fmt.Errorf("invalid bare host-list member %q", part)
			}
		}
		parts[i] = part
	}
	return parts, nil
}

// Restrict the new list surface to foreground commands with captured output.
// Single-target SSH keeps all its existing transport modes.
func rejectHostListModes(runtime []string, policy config.SSHHostConfig) error {
	var modeErr error
	noTTY := false
	sshargs.Walk(runtime, func(option sshargs.Option) bool {
		if strings.ContainsRune("GQONWLRDwfMst", rune(option.Name)) {
			modeErr = fmt.Errorf("SSH option -%c is incompatible with comma-separated hosts", option.Name)
			return false
		}
		if option.Name == 'T' {
			noTTY = true
		}
		return true
	})
	if modeErr != nil {
		return modeErr
	}
	args := connector.ComposeSSHOptions(connector.SSHOptionPlan{Runtime: runtime, Resolved: policy})
	// Preserve normal per-endpoint multiplexing while refusing a configured
	// socket shared across destinations. %% is a literal percent in OpenSSH.
	if path := connector.EffectiveSSHOption(args, "ControlPath"); path != "" && !strings.EqualFold(path, "none") {
		tokens := strings.ReplaceAll(path, "%%", "")
		scoped := strings.Contains(tokens, "%C") || (strings.Contains(tokens, "%h") && strings.Contains(tokens, "%p") && strings.Contains(tokens, "%r"))
		if !scoped {
			return fmt.Errorf("host-list ControlPath must contain %%C or all of %%h, %%p and %%r")
		}
	}
	for _, name := range []string{"LocalForward", "RemoteForward", "DynamicForward", "RemoteCommand"} {
		if value := connector.EffectiveSSHOption(args, name); value != "" && !strings.EqualFold(value, "none") {
			return fmt.Errorf("SSH option %s is incompatible with comma-separated hosts", name)
		}
	}
	allowed := map[string][]string{
		"SessionType":             {"", "default"},
		"ForkAfterAuthentication": {"", "no"},
		"Tunnel":                  {"", "no"},
		"ControlMaster":           {"", "no", "auto"},
	}
	if !noTTY {
		allowed["RequestTTY"] = []string{"", "no", "auto"}
	}
	for key, values := range allowed {
		value := strings.ToLower(connector.EffectiveSSHOption(args, key))
		ok := false
		for _, permitted := range values {
			if value == permitted {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("SSH option %s=%s is incompatible with comma-separated hosts", key, value)
		}
	}
	if value := connector.EffectiveSSHOption(args, "Port"); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("invalid SSH port %q", value)
		}
	}
	return nil
}
