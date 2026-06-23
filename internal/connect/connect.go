package connect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/audit"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/highlight"
	"github.com/ntwrknrd/nssh/internal/ui"
)

const maxCompatibilityFixIterations = 5

type connectionResult struct {
	Err    error
	Output string
}

// ConnectHost handles an interactive SSH connection.
type Options struct {
	Verbosity    int
	SSHVerbosity int
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

	configTimer := connector.StartTiming(connector.TimingConfigLoad)
	explicitUser := extractExplicitUser(hostname, sshArgs)
	resolved, err := ResolveHostForConnect(hostname, explicitUser)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	cfg := resolved.Config
	configTimer.Emit()

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

	configTimer := connector.StartTiming(connector.TimingConfigLoad)
	explicitUser := extractExplicitUser(hostname, sshArgs)
	resolved, err := ResolveLiteralHostForConnect(hostname, explicitUser)
	if err != nil {
		return fmt.Errorf("resolve literal host: %w", err)
	}
	cfg := resolved.Config
	configTimer.Emit()

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
	conn.SetHighlightOptions(highlightOptionsFromConfig(resolved.Highlight))
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
	conn.SetHighlightOptions(highlightOptionsFromConfig(resolved.Highlight))
	conn.SetResolvedEndpoint(resolved.Hostname, fmt.Sprintf("%d", resolved.Port))
	conn.SetAcceptOnceMode(cfg.SSH.Security.AcceptOnceMode)
	conn.SetTimeouts(&cfg.SSH.Connection)
	return conn
}

func highlightOptionsFromConfig(cfg config.HighlightConfig) highlight.Options {
	return highlight.Options{
		Enabled: cfg.EnabledValue(),
		Profile: cfg.EffectiveProfile(),
	}
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
