package connect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/audit"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/askpass"
	"github.com/ntwrknrd/nssh/internal/ssh/captured"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	sshhighlight "github.com/ntwrknrd/nssh/internal/ssh/highlight"
	"github.com/ntwrknrd/nssh/internal/ui"
)

const maxCompatibilityFixIterations = 5

type Request struct {
	Host          string
	LiteralTarget bool
	SSHArgs       []string
	RemoteCommand []string
	Options       Options
}

type connectionResult struct {
	Err    error
	Output string
}

// ConnectHost handles an interactive SSH connection.
type Options struct {
	Verbosity    int
	SSHVerbosity int
}

var (
	resolveHostnameFunc         = ResolveHostname
	connectHostFunc             = ConnectHost
	connectLiteralHostFunc      = ConnectLiteralHost
	runRemoteCommandFunc        = RunRemoteCommand
	runLiteralRemoteCommandFunc = RunLiteralRemoteCommand
	runCapturedCommandFunc      = func(ctx context.Context, req captured.Request) (captured.Result, error) {
		return captured.Runner{}.Run(ctx, req)
	}
	askpassHelperPathFunc = defaultAskpassHelperPath
)

func ConnectRequest(ctx context.Context, req Request) error {
	if len(req.RemoteCommand) > 0 {
		if req.LiteralTarget {
			return runLiteralRemoteCommandFunc(ctx, req.Host, req.SSHArgs, req.RemoteCommand, req.Options)
		}
		hostname, err := resolveHostnameFunc(req.Host)
		if err != nil {
			return err
		}
		return runRemoteCommandFunc(ctx, hostname, req.SSHArgs, req.RemoteCommand, req.Options)
	}

	if req.LiteralTarget {
		return connectLiteralHostFunc(ctx, req.Host, req.SSHArgs, req.Options)
	}
	hostname, err := resolveHostnameFunc(req.Host)
	if err != nil {
		return err
	}
	return connectHostFunc(ctx, hostname, req.SSHArgs, req.Options)
}

func ConnectHost(ctx context.Context, hostname string, sshArgs []string, opts ...Options) error {
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
	recorded, err := maybeWrapWithRecording(hostname, sshArgs, options)
	if err != nil {
		return err
	}
	if recorded {
		return nil
	}

	explicitUser := extractExplicitUser(hostname, sshArgs)
	resolved, err := ResolveHostForConnect(hostname, explicitUser)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	cfg := resolved.Config

	audit := newConnectAudit(cfg)
	if audit != nil {
		defer func() { _ = audit.Close() }()
	}

	slog.Debug("connecting to host",
		"host", resolved.Hostname,
		"timeout", cfg.SSH.Connection.Timeout.Duration(),
	)

	if audit != nil {
		audit.Info("ssh_connect_start", "host", resolved.Hostname, "ssh_args", sshArgs)
	}

	result := runResolvedConnection(ctx, resolved, sshArgs, cfg, audit, options)
	if result.Err != nil && isCompatibilityError(result.Err) {
		var err error
		if result, resolved, err = handleCompatibilityFixes(ctx, hostname, explicitUser, resolved, sshArgs, cfg, audit, options, result); err != nil {
			return err
		}
	}

	return result.Err
}

func RunRemoteCommand(ctx context.Context, hostname string, sshArgs, command []string, opts ...Options) error {
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
	explicitUser := extractExplicitUser(hostname, sshArgs)
	resolved, err := ResolveHostForConnect(hostname, explicitUser)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	cfg := resolved.Config

	audit := newConnectAudit(cfg)
	if audit != nil {
		defer func() { _ = audit.Close() }()
		audit.Info("ssh_remote_command_start", "host", resolved.Hostname, "ssh_args", sshArgs, "command", command)
	}
	err = runResolvedRemoteCommand(ctx, resolved, sshArgs, command, cfg, options)
	if audit != nil {
		if err == nil {
			audit.Info("ssh_remote_command_end", "host", resolved.Hostname, "status", "success")
		} else {
			exitCode := 1
			var exitErr *exit.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.Code
			}
			audit.Info("ssh_remote_command_end", "host", resolved.Hostname, "status", "error", "exit_code", exitCode, "error", err.Error())
		}
	}
	return err
}

func RunLiteralRemoteCommand(ctx context.Context, hostname string, sshArgs, command []string, opts ...Options) error {
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
	explicitUser := extractExplicitUser(hostname, sshArgs)
	resolved, err := ResolveLiteralHostForConnect(hostname, explicitUser)
	if err != nil {
		return fmt.Errorf("resolve literal host: %w", err)
	}
	cfg := resolved.Config

	audit := newConnectAudit(cfg)
	if audit != nil {
		defer func() { _ = audit.Close() }()
		audit.Info("ssh_remote_command_start", "host", resolved.Hostname, "ssh_args", sshArgs, "command", command)
	}
	err = runResolvedRemoteCommand(ctx, resolved, sshArgs, command, cfg, options)
	if audit != nil {
		if err == nil {
			audit.Info("ssh_remote_command_end", "host", resolved.Hostname, "status", "success")
		} else {
			exitCode := 1
			var exitErr *exit.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.Code
			}
			audit.Info("ssh_remote_command_end", "host", resolved.Hostname, "status", "error", "exit_code", exitCode, "error", err.Error())
		}
	}
	return err
}

func runResolvedRemoteCommand(ctx context.Context, resolved *ResolvedHost, sshArgs, command []string, cfg *config.Config, opts Options) error {
	if resolved == nil {
		return fmt.Errorf("resolved host is required")
	}
	req := captured.Request{
		Hostname:      resolved.Hostname,
		Username:      resolved.Username,
		Port:          resolved.Port,
		SSHOptions:    resolved.SSH,
		SSHVerbosity:  opts.SSHVerbosity,
		SSHArgs:       sshArgs,
		RemoteCommand: command,
	}
	if cfg != nil && cfg.SSH.Connection.Timeout.Duration() > 0 {
		req.Timeout = cfg.SSH.Connection.Timeout.Duration()
	}
	var askpassServer *askpass.Server
	var askpassCancel context.CancelFunc
	var askpassDone chan error
	if resolved.Credential != nil {
		askpassTimer := connector.StartTiming(connector.TimingAskpassSetup)
		password, err := remoteCommandPassword(ctx, resolved.Credential)
		if err != nil {
			askpassTimer.Emit()
			return err
		}
		if password != nil {
			defer password.Destroy()
			helper, err := askpassHelperPathFunc()
			if err != nil {
				askpassTimer.Emit()
				return err
			}
			askpassServer, err = askpass.NewServer(password)
			if err != nil {
				askpassTimer.Emit()
				return err
			}
			req.Env = append(req.Env, askpassServer.Env(helper)...)
			askpassCtx, cancel := context.WithCancel(ctx)
			askpassCancel = cancel
			askpassDone = make(chan error, 1)
			go func() {
				askpassDone <- askpassServer.ServeOnce(askpassCtx)
			}()
		}
		askpassTimer.Emit()
	}
	if askpassServer != nil {
		defer func() {
			askpassCancel()
			_ = askpassServer.Close()
			select {
			case err := <-askpassDone:
				if err != nil {
					slog.Debug("askpass server stopped", "err", err)
				}
			case <-time.After(time.Second):
				slog.Debug("askpass server did not stop within timeout")
			}
		}()
	}

	result, err := runCapturedCommandFunc(ctx, req)
	writeCapturedCommandOutput(result, resolved.Highlight)
	return err
}

func writeCapturedCommandOutput(result captured.Result, cfg config.HighlightConfig) {
	if len(result.Output) == 0 {
		stdout := applyRemoteCommandHighlight(result.Stdout, cfg)
		if len(stdout) > 0 {
			if _, writeErr := os.Stdout.Write(stdout); writeErr != nil {
				slog.Debug("failed to write remote stdout", "err", writeErr)
			}
		}
		if len(result.Stderr) > 0 {
			if _, writeErr := os.Stderr.Write(result.Stderr); writeErr != nil {
				slog.Debug("failed to write remote stderr", "err", writeErr)
			}
		}
		return
	}

	highlighter := sshhighlight.New(sshhighlight.Options{
		Enabled: cfg.EnabledValue(),
		Profile: cfg.EffectiveProfile(),
	})
	for _, event := range result.Output {
		switch event.Stream {
		case captured.StreamStdout:
			data := event.Data
			if highlighter != nil {
				data = highlighter.Highlight(data)
			}
			if len(data) > 0 {
				if _, writeErr := os.Stdout.Write(data); writeErr != nil {
					slog.Debug("failed to write remote stdout", "err", writeErr)
				}
			}
		case captured.StreamStderr:
			if len(event.Data) > 0 {
				if _, writeErr := os.Stderr.Write(event.Data); writeErr != nil {
					slog.Debug("failed to write remote stderr", "err", writeErr)
				}
			}
		}
	}
}

func remoteCommandPassword(ctx context.Context, cred *ResolvedCredential) (*secret.Secret, error) {
	if cred == nil {
		return nil, nil
	}
	if cred.Password != nil {
		return cred.Password, nil
	}
	if cred.PasswordResolver == nil {
		return nil, nil
	}
	password, err := cred.PasswordResolver(ctx)
	if err != nil {
		return nil, err
	}
	return password, nil
}

func applyRemoteCommandHighlight(data []byte, cfg config.HighlightConfig) []byte {
	highlighter := sshhighlight.New(sshhighlight.Options{
		Enabled: cfg.EnabledValue(),
		Profile: cfg.EffectiveProfile(),
	})
	if highlighter == nil {
		return data
	}
	return highlighter.Highlight(data)
}

func defaultAskpassHelperPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate nssh executable: %w", err)
	}
	helper := filepath.Join(filepath.Dir(exe), "nssh-askpass")
	if _, err := os.Stat(helper); err != nil {
		return "", fmt.Errorf("nssh-askpass not found beside nssh: %w", err)
	}
	return helper, nil
}

func ConnectLiteralHost(ctx context.Context, hostname string, sshArgs []string, opts ...Options) error {
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
	recorded, err := maybeWrapWithRecording(hostname, sshArgs, options)
	if err != nil {
		return err
	}
	if recorded {
		return nil
	}

	explicitUser := extractExplicitUser(hostname, sshArgs)
	resolved, err := ResolveLiteralHostForConnect(hostname, explicitUser)
	if err != nil {
		return fmt.Errorf("resolve literal host: %w", err)
	}
	cfg := resolved.Config

	audit := newConnectAudit(cfg)
	if audit != nil {
		defer func() { _ = audit.Close() }()
		audit.Info("ssh_connect_start", "host", resolved.Hostname, "ssh_args", sshArgs)
	}

	result := runResolvedConnection(ctx, resolved, sshArgs, cfg, audit, options)
	if result.Err != nil && resolved.Provider != "" && isCompatibilityError(result.Err) {
		var compatErr error
		if result, resolved, compatErr = handleCompatibilityFixes(ctx, hostname, explicitUser, resolved, sshArgs, cfg, audit, options, result); compatErr != nil {
			return compatErr
		}
	}
	return result.Err
}

func newConnectAudit(cfg *config.Config) *audit.Logger {
	if cfg == nil || !cfg.Logging.Audit.Enabled {
		return nil
	}
	paths := config.DefaultPaths()
	logger, err := audit.NewLogger(slog.LevelError, &cfg.Logging.Audit, paths.StateDir)
	if err != nil {
		slog.Warn("failed to initialize audit logger", "err", err)
		return nil
	}
	return logger
}

func runResolvedConnection(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, audit *audit.Logger, opts Options) connectionResult {
	conn := newConnector(resolved, sshArgs, cfg, opts)
	connErr := conn.Run(ctx)
	result := connectionResult{Err: connErr, Output: conn.LastOutput()}

	if audit != nil {
		if connErr == nil {
			audit.Info("ssh_connect_end", "host", resolved.Hostname, "status", "success")
		} else {
			exitCode := 1
			var exitErr *exit.ExitError
			if errors.As(connErr, &exitErr) {
				exitCode = exitErr.Code
			}
			audit.Info("ssh_connect_end", "host", resolved.Hostname, "status", "error", "exit_code", exitCode, "error", connErr.Error())
		}
	}

	return result
}

func handleCompatibilityFixes(ctx context.Context, query, explicitUser string, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, audit *audit.Logger, opts Options, result connectionResult) (connectionResult, *ResolvedHost, error) {
	for iteration := 1; iteration <= maxCompatibilityFixIterations; iteration++ {
		fixes := discoverCompatibilityFixes(ctx, resolved, cfg, result.Output)
		if len(fixes) == 0 {
			slog.Debug("connection failed with possible compatibility issue; configure inventory host ssh.compatibility in nssh YAML", "host", resolved.Hostname)
			return result, resolved, nil
		}
		if !confirmCompatibilityFixes(iteration, fixes) {
			return result, resolved, nil
		}
		if err := appendResolvedHostCompatFixes(cfg, resolved, fixes); err != nil {
			return result, resolved, err
		}
		if err := config.SaveInventoryProviderHost(config.DefaultPaths().ConfigFile, cfg, resolved.Provider, resolved.Canonical); err != nil {
			return result, resolved, err
		}
		ui.Success("Compatibility fixes applied")
		nextResolved, err := ResolveHostForConnect(query, explicitUser, cfg)
		if err != nil {
			return result, resolved, err
		}
		resolved = nextResolved
		result = runResolvedConnection(ctx, resolved, sshArgs, cfg, audit, opts)
		if result.Err == nil || !isCompatibilityError(result.Err) {
			return result, resolved, nil
		}
	}
	return result, resolved, nil
}

func discoverCompatibilityFixes(ctx context.Context, resolved *ResolvedHost, cfg *config.Config, output string) []compat.FloorSelection {
	if resolved == nil {
		return nil
	}
	candidateSSH := resolved.SSH
	found := make([]compat.FloorSelection, 0)
	nextOutput := output
	for iteration := 1; iteration <= maxCompatibilityFixIterations; iteration++ {
		fixes := compatibilityFixesToApply(candidateSSH, nextOutput)
		if len(fixes) == 0 {
			nextOutput = probeCompatibilityOutput(ctx, resolved, cfg, candidateSSH)
			if nextOutput == "" {
				return found
			}
			fixes = compatibilityFixesToApply(candidateSSH, nextOutput)
			if len(fixes) == 0 {
				return found
			}
		}
		for _, fix := range fixes {
			found = append(found, fix)
			setCompatibilityFloor(&candidateSSH.Compatibility, fix)
		}
		nextOutput = ""
	}
	return found
}

func probeCompatibilityOutput(ctx context.Context, resolved *ResolvedHost, cfg *config.Config, sshOptions config.SSHHostConfig) string {
	testCfg := connector.TestConfig{
		Timeout:    10 * time.Second,
		Port:       fmt.Sprintf("%d", resolved.Port),
		SSHOptions: sshOptions,
	}
	if cfg != nil {
		testCfg.UseSystemKnownHosts = cfg.SSH.Security.CompatPersistProbes
		if cfg.SSH.Connection.Timeout.Duration() > 0 {
			testCfg.Timeout = cfg.SSH.Connection.Timeout.Duration()
		}
	}
	testResult, err := connector.TestConnection(ctx, resolved.Hostname, resolved.Username, testCfg)
	if err != nil || testResult == nil {
		if err != nil {
			slog.Debug("compatibility probe failed", "err", err)
		}
		return ""
	}
	return testResult.Stderr
}

func compatibilityFixesToApply(sshConfig config.SSHHostConfig, output string) []compat.FloorSelection {
	issues := compat.ParseNegotiationIssues(output)
	fixes := make([]compat.FloorSelection, 0, len(issues))
	for _, issue := range issues {
		if compatibilityFloor(sshConfig.Compatibility, issue.Category) != "" {
			continue
		}
		fix, ok := compat.SelectCompatibilityFloor(issue, compat.LocalSupportedAlgorithms(issue.Category))
		if ok && !slices.Contains(fixes, fix) {
			fixes = append(fixes, fix)
		}
	}
	return fixes
}

func confirmCompatibilityFixes(iteration int, fixes []compat.FloorSelection) bool {
	if iteration == 1 {
		fmt.Println()
		ui.Info("Detected legacy SSH compatibility issues:")
	} else {
		ui.Info("Applying additional fixes:")
	}
	for _, fix := range fixes {
		fmt.Printf("    - %s: %s\n", fix.Category, fix.Floor)
	}
	fmt.Println()
	confirmed, err := ui.Confirm("Apply compatibility fixes to nssh config?", true)
	return err == nil && confirmed
}

func appendResolvedHostCompatFixes(cfg *config.Config, resolved *ResolvedHost, fixes []compat.FloorSelection) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if resolved == nil {
		return fmt.Errorf("resolved host is required")
	}
	providerName := strings.TrimSpace(resolved.Provider)
	hostName := strings.TrimSpace(resolved.Canonical)
	if hostName == "" {
		hostName = strings.TrimSpace(resolved.Hostname)
	}
	if providerName == "" || hostName == "" {
		return fmt.Errorf("resolved host is missing provider or canonical hostname")
	}
	provider, ok := cfg.Inventory.Providers[providerName]
	if !ok {
		provider, ok = cfg.Inventory.Provider[providerName]
	}
	if !ok {
		return fmt.Errorf("inventory provider %q is not configured", providerName)
	}
	if provider.Hosts == nil {
		provider.Hosts = make(map[string]config.InventoryHostConfig)
	}
	host := provider.Hosts[hostName]
	if host.Group == "" {
		host.Group = resolved.Group
	}
	for _, fix := range fixes {
		setCompatibilityFloor(&host.SSH.Compatibility, fix)
	}
	provider.Hosts[hostName] = host
	cfg.Inventory.Providers[providerName] = provider
	if cfg.Inventory.Provider == nil {
		cfg.Inventory.Provider = make(map[string]config.InventoryProviderConfig)
	}
	cfg.Inventory.Provider[providerName] = provider
	return nil
}

func compatibilityFloor(compatibility config.SSHCompatibility, category compat.Category) string {
	switch category {
	case compat.CategoryKex:
		return compatibility.Kex
	case compat.CategoryMAC:
		return compatibility.MAC
	case compat.CategoryHostKey:
		return compatibility.HostKey
	case compat.CategoryPublicKey:
		return compatibility.PublicKey
	default:
		return ""
	}
}

func setCompatibilityFloor(compatibility *config.SSHCompatibility, fix compat.FloorSelection) {
	if compatibility == nil || fix.Floor == "" {
		return
	}
	switch fix.Category {
	case compat.CategoryKex:
		compatibility.Kex = fix.Floor
	case compat.CategoryMAC:
		compatibility.MAC = fix.Floor
	case compat.CategoryHostKey:
		compatibility.HostKey = fix.Floor
	case compat.CategoryPublicKey:
		compatibility.PublicKey = fix.Floor
	}
}

func retryResolvedConnection(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options) error {
	var retryPassword *secret.Secret
	retryResolved, retryErr := ResolveHostForConnect(resolved.Hostname, resolved.Username, cfg)
	if retryErr != nil {
		slog.Warn("credential re-resolution failed", "err", retryErr)
	} else if retryResolved.Credential != nil {
		retryPassword = retryResolved.Credential.Password
	}

	conn := connector.NewConnector(resolved.Hostname, resolved.Username, retryPassword, sshArgs)
	if retryResolved != nil && retryResolved.Credential != nil {
		conn.SetPasswordResolver(retryResolved.Credential.PasswordResolver)
	}
	conn.SetHostKeyPromptFunc(newHostKeyPromptFunc())
	conn.SetSSHOptions(resolved.SSH)
	conn.SetAcceptOnceMode(cfg.SSH.Security.AcceptOnceMode)
	conn.SetTimeouts(&cfg.SSH.Connection)
	conn.SetSSHVerbosity(opts.SSHVerbosity)
	conn.SetResolvedEndpoint(resolved.Hostname, fmt.Sprintf("%d", resolved.Port))
	return conn.Run(ctx)
}

func newConnector(resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options) *connector.Connector {
	var password *secret.Secret
	if resolved.Credential != nil {
		password = resolved.Credential.Password
		slog.Debug("resolved credential", "username", resolved.Username, "source", resolved.Credential.Source)
	}
	conn := connector.NewConnector(resolved.Hostname, resolved.Username, password, sshArgs)
	if resolved.Credential != nil {
		conn.SetPasswordResolver(resolved.Credential.PasswordResolver)
	}
	conn.SetHostKeyPromptFunc(newHostKeyPromptFunc())
	conn.SetSSHOptions(resolved.SSH)
	conn.SetSSHVerbosity(opts.SSHVerbosity)
	conn.SetResolvedEndpoint(resolved.Hostname, fmt.Sprintf("%d", resolved.Port))
	conn.SetAcceptOnceMode(cfg.SSH.Security.AcceptOnceMode)
	conn.SetTimeouts(&cfg.SSH.Connection)
	return conn
}

func isCompatibilityError(err error) bool {
	var exitErr *exit.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code == exit.ExitConnectionFailed || exitErr.Code == 255
	}
	return false
}

func extractExplicitUser(hostname string, sshArgs []string) string {
	for i, arg := range sshArgs {
		if arg == "-l" && i+1 < len(sshArgs) {
			return sshArgs[i+1]
		}
		if strings.HasPrefix(arg, "-l") && len(arg) > 2 {
			return arg[2:]
		}
	}
	if idx := strings.LastIndex(hostname, "@"); idx != -1 {
		return hostname[:idx]
	}
	return ""
}
