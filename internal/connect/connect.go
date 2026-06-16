package connect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ntwrknrd/nssh/internal/audit"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

// ConnectHost handles an interactive SSH connection.
type Options struct {
	SSHVerbosity int
}

func ConnectHost(ctx context.Context, hostname string, sshArgs []string, opts ...Options) error {
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
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

	connErr := runResolvedConnection(ctx, resolved, sshArgs, cfg, audit, options)
	if connErr != nil && isCompatibilityError(connErr) {
		slog.Debug("connection failed with possible compatibility issue; configure inventory host ssh.compat in nssh YAML", "host", resolved.Hostname)
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

func runResolvedConnection(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, audit *audit.Logger, opts Options) error {
	conn := newConnector(resolved, sshArgs, cfg, opts)
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
