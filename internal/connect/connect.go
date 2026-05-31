package connect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/audit"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// ConnectHost handles an interactive SSH connection.
func ConnectHost(ctx context.Context, hostname string, sshArgs []string) error {
	recorded, err := maybeWrapWithRecording(hostname, sshArgs)
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
		"ssh_args", sshArgs,
		"timeout", cfg.SSH.Connection.Timeout.Duration(),
	)

	if audit != nil {
		audit.Info("ssh_connect_start", "host", resolved.Hostname, "ssh_args", sshArgs)
	}

	connErr := runResolvedConnection(ctx, resolved, sshArgs, cfg, audit)
	if connErr != nil && isCompatibilityError(connErr) {
		if handleCompatibilityFix(ctx, resolved.Hostname, resolved.IncludeFile) {
			slog.Debug("retrying connection after compatibility fixes")
			return retryResolvedConnection(ctx, resolved, sshArgs, cfg)
		}
	}

	return connErr
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

func runResolvedConnection(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, audit *audit.Logger) error {
	conn := newConnector(resolved, sshArgs, cfg)
	connErr := conn.Run(ctx)

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

	return connErr
}

func retryResolvedConnection(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config) error {
	var retryPassword *secret.Secret
	retryResolved, retryErr := ResolveHostForConnect(resolved.Hostname, resolved.Username, cfg)
	if retryErr != nil {
		slog.Warn("credential re-resolution failed", "err", retryErr)
	} else if retryResolved.Credential != nil {
		retryPassword = retryResolved.Credential.Password
	}

	conn := connector.NewConnector(resolved.Hostname, resolved.Username, retryPassword, sshArgs)
	conn.SetHostKeyPromptFunc(newHostKeyPromptFunc())
	conn.SetAcceptOnceMode(cfg.SSH.Security.AcceptOnceMode)
	conn.SetTimeouts(&cfg.SSH.Connection)
	return conn.Run(ctx)
}

func newConnector(resolved *ResolvedHost, sshArgs []string, cfg *config.Config) *connector.Connector {
	var password *secret.Secret
	if resolved.Credential != nil {
		password = resolved.Credential.Password
		slog.Debug("resolved credential", "username", resolved.Username, "source", resolved.Credential.Source)
	}
	conn := connector.NewConnector(resolved.Hostname, resolved.Username, password, sshArgs)
	conn.SetHostKeyPromptFunc(newHostKeyPromptFunc())
	if resolved.HostEntry != nil {
		conn.SetResolvedEndpoint(resolved.HostEntry.HostName, resolved.HostEntry.Port())
	}
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

func handleCompatibilityFix(ctx context.Context, hostname, includeFile string) bool {
	const maxIterations = 5

	parser := sshconfig.NewParser()
	hostEntry, parsedCfg, err := parser.FindHostWithLocation(hostname)
	if err != nil || hostEntry == nil {
		slog.Debug("host not found in config, cannot apply compat fixes", "host", hostname)
		return false
	}

	providerState, err := inventory.LoadProviderStateByIncludeFile(includeFile)
	if err != nil {
		slog.Debug("provider state lookup failed", "include_file", includeFile, "err", err)
	}
	isProviderManaged := providerState != nil && providerState.FindHost(hostname) != nil

	var allFixesApplied []compat.CompatType
	appliedSet := make(map[compat.CompatType]bool)

	for iteration := 1; iteration <= maxIterations; iteration++ {
		testCfg := connector.TestConfig{
			Timeout: 10 * time.Second,
			Port:    hostEntry.Port(),
		}
		if cfg, err := config.LoadDefault(); err == nil && cfg != nil {
			testCfg.UseSystemKnownHosts = cfg.SSH.Security.CompatPersistProbes
		}
		resolved, err := ResolveHostForConnect(hostEntry.Host, "")
		if err == nil && resolved.Credential != nil {
			testCfg.Password = resolved.Credential.Password
		}

		testUser := ""
		if resolved != nil {
			testUser = resolved.Username
		}
		testResult, err := connector.TestConnection(ctx, hostEntry.Host, testUser, testCfg)
		if err != nil {
			slog.Debug("test connection failed", "err", err)
			break
		}

		if testResult.Success || compat.IsAuthFailureAfterKex(testResult.Stderr) {
			slog.Debug("KEX succeeded, not a compatibility issue")
			break
		}

		compatTypes := compat.ParseCompatibilityError(testResult.Stderr)
		if len(compatTypes) == 0 {
			slog.Debug("no compatibility issues detected in iteration", "iteration", iteration)
			break
		}

		var newFixes []compat.CompatType
		for _, ct := range compatTypes {
			if !appliedSet[ct] {
				newFixes = append(newFixes, ct)
			}
		}

		if len(newFixes) == 0 {
			slog.Debug("no new fixes to apply", "iteration", iteration)
			break
		}

		if iteration == 1 {
			fmt.Println()
			ui.Info("Detected legacy SSH compatibility issues:")
			for _, ct := range newFixes {
				compatCfg := compat.CompatConfigs[ct]
				fmt.Printf("    - %s\n", compatCfg.Name)
			}
			fmt.Println()

			confirmed, err := ui.Confirm("Apply compatibility fixes to SSH config?", true)
			if err != nil || !confirmed {
				return false
			}
		} else {
			ui.Info("Applying additional fixes:")
			for _, ct := range newFixes {
				compatCfg := compat.CompatConfigs[ct]
				fmt.Printf("    - %s\n", compatCfg.Name)
			}
		}

		if isProviderManaged {
			if err := inventory.PersistCompatFixes(providerState.IncludeFile, hostname, newFixes); err != nil {
				ui.Error("Failed to persist provider compat fixes: %s", err)
				return false
			}

			hostEntry, parsedCfg, err = parser.FindHostWithLocation(hostname)
			if err != nil || hostEntry == nil {
				ui.Error("Failed to reload provider-managed host after compat fix: %v", err)
				return false
			}
		} else {
			if err := sshconfig.ApplyCompatFixes(hostEntry, newFixes); err != nil {
				ui.Error("Failed to apply fixes: %s", err)
				return false
			}

			if err := parser.WriteFile(parsedCfg); err != nil {
				ui.Error("Failed to write config: %s", err)
				return false
			}
		}

		for _, ct := range newFixes {
			appliedSet[ct] = true
			allFixesApplied = append(allFixesApplied, ct)
		}

		slog.Debug("applied compat fixes", "iteration", iteration, "fixes", len(newFixes))
	}

	if len(allFixesApplied) > 0 {
		ui.Success("Compatibility fixes applied")
		fmt.Println()
		return true
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
