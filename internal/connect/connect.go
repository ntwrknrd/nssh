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
	"sync"
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
	sshpassword "github.com/ntwrknrd/nssh/internal/ssh/password"
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
	runControlCommandFunc       = RunControlCommand
	runCapturedCommandFunc      = func(ctx context.Context, req captured.Request) (captured.Result, error) {
		return captured.Runner{}.Run(ctx, req)
	}
	askpassHelperPathFunc = defaultAskpassHelperPath
	checkMuxSessionFunc   = func(ctx context.Context, req connector.MuxCheckRequest) (bool, bool) {
		return connector.CheckMuxSession(ctx, req, nil)
	}
	startMuxSessionFunc = func(ctx context.Context, req connector.MuxStartRequest) error {
		return connector.StartMuxSession(ctx, req, nil)
	}
	hostKeyProbeFunc          = probeInteractiveHostKey
	hostKeyPrepareFunc        = runHostKeyPreparation
	hostKeyPromptFunc         = newHostKeyPromptFunc
	scanHostKeyFunc           = scanHostKey
	removeKnownHostsEntryFunc = removeKnownHostsEntry
)

func ConnectRequest(ctx context.Context, req Request) error {
	if len(req.RemoteCommand) > 0 {
		if req.LiteralTarget {
			return runLiteralRemoteCommandFunc(ctx, req.Host, req.SSHArgs, req.RemoteCommand, req.Options)
		}
		return runRemoteCommandFunc(ctx, req.Host, req.SSHArgs, req.RemoteCommand, req.Options)
	}

	if _, _, ok := connector.ExtractControlCommand(req.SSHArgs); ok {
		return runControlCommandFunc(ctx, req.Host, req.LiteralTarget, req.SSHArgs, req.Options)
	}

	if req.LiteralTarget {
		return connectLiteralHostFunc(ctx, req.Host, req.SSHArgs, req.Options)
	}
	return connectHostFunc(ctx, req.Host, req.SSHArgs, req.Options)
}

func ConnectHost(ctx context.Context, hostname string, sshArgs []string, opts ...Options) error {
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
	explicitUser := extractExplicitUser(hostname, sshArgs)
	resolved, err := ResolveSmartHostForConnect(hostname, explicitUser)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	cfg := resolved.Config

	recorded, err := maybeWrapWithRecording(resolved.Canonical, sshArgs, options)
	if err != nil {
		return err
	}
	if recorded {
		return nil
	}

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
		if result, resolved, err = handleCompatibilityFixes(ctx, hostname, explicitUser, true, resolved, sshArgs, cfg, audit, options, result); err != nil {
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
	resolved, err := ResolveSmartHostForConnect(hostname, explicitUser)
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

func RunControlCommand(ctx context.Context, hostname string, literal bool, sshArgs []string, opts ...Options) error {
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
	controlCommand, controlSSHArgs, ok := connector.ExtractControlCommand(sshArgs)
	if !ok {
		return fmt.Errorf("ssh control command is required")
	}
	explicitUser := extractExplicitUser(hostname, controlSSHArgs)
	var resolved *ResolvedHost
	var err error
	if literal {
		resolved, err = ResolveLiteralHostForConnect(hostname, explicitUser)
		if err != nil {
			return fmt.Errorf("resolve literal host: %w", err)
		}
	} else {
		resolved, err = ResolveSmartHostForConnect(hostname, explicitUser)
		if err != nil {
			return fmt.Errorf("resolve host: %w", err)
		}
	}
	return connector.RunControlCommand(ctx, controlCommandRequest(resolved, controlCommand, controlSSHArgs, resolved.Config, options), nil)
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
	passwordFuture, muxHot := preparePasswordPrefetch(ctx, resolved, sshArgs, cfg, opts)
	if passwordFuture != nil {
		defer passwordFuture.Close()
	}
	proxyPasswordFuture := prepareProxyPasswordFuture(resolved)
	if proxyPasswordFuture != nil {
		defer proxyPasswordFuture.Close()
	}
	defer destroyImmediateResolvedPasswords(resolved)
	passwordResolvers := interactiveAskpassResolvers(resolved, passwordFuture, proxyPasswordFuture)
	var askpassHandle *connectionAskpassHandle
	if passwordResolvers.any() && !muxHot {
		askpassTimer := connector.StartTiming(connector.TimingAskpassSetup)
		var err error
		askpassHandle, err = startConnectionAskpass(ctx, passwordResolvers)
		askpassTimer.Emit()
		if err != nil {
			return err
		}
		defer askpassHandle.cleanup()
	}
	var hostKeyPrep *connector.HostKeyPreparation
	if passwordResolvers.any() {
		if !muxHot {
			var err error
			hostKeyPrep, err = prepareInteractiveHostKey(ctx, resolved, sshArgs, cfg, opts, proxyAskpassEnv(askpassHandle))
			if err != nil {
				return err
			}
			if hostKeyPrep != nil {
				defer hostKeyPrep.Cleanup()
				req.SSHArgs = appendHostKeyPreparationSSHArgs(req.SSHArgs, hostKeyPrep)
			}
		}
	}

	if !muxHot {
		muxStartReq := muxStartRequest(resolved, req.SSHArgs, cfg, opts, nil)
		if _, ok := connector.BuildMuxStartArgs(muxStartReq); ok {
			if askpassHandle != nil {
				muxStartReq.Env = askpassHandle.env
			}

			// Captured remote commands drain stdout/stderr and reap the foreground ssh process.
			// Starting the persistent master as its own step keeps ControlPersist behavior
			// independent of captured output pipe lifetime.
			if err := startMuxSessionFunc(ctx, muxStartReq); err != nil {
				return err
			}
			muxHot = true
		}
	}

	if askpassHandle != nil && !muxHot {
		req.Env = append(req.Env, askpassHandle.env...)
	}

	slog.Debug("starting captured ssh command", "host", req.Hostname, "has_askpass", len(req.Env) > 0)
	result, err := runCapturedCommandFunc(ctx, req)
	writeCapturedCommandOutput(result, resolved.Highlight)
	slog.Debug("captured ssh command completed", "host", req.Hostname, "err", err)
	return err
}

func startAskpassServerEnv(ctx context.Context, resolve func(context.Context) (*secret.Secret, error)) (*askpass.Server, context.CancelFunc, chan error, []string, error) {
	return startAskpassServerEnvForRole(ctx, resolve, false)
}

// StartProxyAskpassEnvironment starts an isolated askpass channel suitable for
// a managed ProxyCommand. The caller must invoke cleanup after SSH exits.
func StartProxyAskpassEnvironment(ctx context.Context, resolve func(context.Context) (*secret.Secret, error)) ([]string, func(), error) {
	return startExternalAskpassEnvironment(ctx, resolve, true)
}

// StartTargetAskpassEnvironment starts the standard target askpass channel for
// callers that execute OpenSSH outside the normal connection runner.
func StartTargetAskpassEnvironment(ctx context.Context, resolve func(context.Context) (*secret.Secret, error)) ([]string, func(), error) {
	return startExternalAskpassEnvironment(ctx, resolve, false)
}

func startExternalAskpassEnvironment(ctx context.Context, resolve func(context.Context) (*secret.Secret, error), proxy bool) ([]string, func(), error) {
	server, cancel, done, env, err := startAskpassServerEnvForRole(ctx, resolve, proxy)
	if err != nil {
		return nil, func() {}, err
	}
	var once sync.Once
	return env, func() {
		once.Do(func() { stopAskpassServer(server, cancel, done) })
	}, nil
}

func startAskpassServerEnvForRole(ctx context.Context, resolve func(context.Context) (*secret.Secret, error), proxy bool) (*askpass.Server, context.CancelFunc, chan error, []string, error) {
	helper, err := askpassHelperPathFunc()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	askpassServer, err := askpass.NewServerWithResolver(resolve)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	askpassCtx, cancel := context.WithCancel(ctx)
	askpassDone := make(chan error, 1)
	go func() {
		askpassDone <- askpassServer.Serve(askpassCtx)
	}()
	env := askpassServer.Env(helper)
	if proxy {
		env = askpassServer.ProxyEnv(helper)
	}
	return askpassServer, cancel, askpassDone, env, nil
}

func stopAskpassServer(server *askpass.Server, cancel context.CancelFunc, done chan error) {
	if cancel != nil {
		cancel()
	}
	if server != nil {
		_ = server.Close()
	}
	if done == nil {
		return
	}
	select {
	case err := <-done:
		if err != nil {
			slog.Debug("askpass server stopped", "err", err)
		}
	case <-time.After(time.Second):
		slog.Debug("askpass server did not stop within timeout")
	}
}

func preparePasswordPrefetch(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options) (*sshpassword.Future, bool) {
	if !shouldPrefetchPassword(resolved) {
		return nil, false
	}
	future := sshpassword.NewFuture(resolved.Credential.PasswordResolver)
	muxHot, _ := checkMuxSessionFunc(ctx, muxCheckRequest(resolved, sshArgs, cfg, opts))
	if !muxHot {
		slog.Debug("starting password prefetch", "host", resolved.Hostname)
		future.Start(ctx)
	} else {
		slog.Debug("skipping password prefetch for hot mux", "host", resolved.Hostname)
	}
	return future, muxHot
}

func shouldPrefetchPassword(resolved *ResolvedHost) bool {
	return resolved != nil &&
		!hasUnmanagedProxyTransport(resolved) &&
		resolved.AuthMode == config.AuthModePassword &&
		resolved.Credential != nil &&
		resolved.Credential.PasswordResolver != nil
}

func muxCheckRequest(resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options) connector.MuxCheckRequest {
	req := connector.MuxCheckRequest{
		Hostname:     resolved.Hostname,
		Username:     resolved.Username,
		Port:         resolved.Port,
		SSHOptions:   resolved.SSH,
		SSHVerbosity: opts.SSHVerbosity,
		SSHArgs:      sshArgs,
	}
	if cfg != nil && cfg.SSH.Connection.Timeout.Duration() > 0 {
		req.Timeout = int(cfg.SSH.Connection.Timeout.Duration().Seconds())
	}
	return req
}

type passwordResolvers struct {
	target func(context.Context) (*secret.Secret, error)
	proxy  func(context.Context) (*secret.Secret, error)
}

func (r passwordResolvers) any() bool {
	return r.target != nil || r.proxy != nil
}

type runningAskpassServer struct {
	server *askpass.Server
	cancel context.CancelFunc
	done   chan error
}

type connectionAskpassHandle struct {
	env     []string
	servers []runningAskpassServer
}

func (h *connectionAskpassHandle) cleanup() {
	if h == nil {
		return
	}
	for _, running := range h.servers {
		stopAskpassServer(running.server, running.cancel, running.done)
	}
}

func proxyAskpassEnv(handle *connectionAskpassHandle) []string {
	if handle == nil {
		return nil
	}
	var env []string
	for _, entry := range handle.env {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case askpass.ProxyHelperEnv, askpass.ProxyRequireEnv, askpass.ProxySocketEnv, askpass.ProxyNonceEnv:
			env = append(env, entry)
		}
	}
	return env
}

func startConnectionAskpass(ctx context.Context, resolvers passwordResolvers) (*connectionAskpassHandle, error) {
	if !resolvers.any() {
		return nil, nil
	}
	handle := &connectionAskpassHandle{}

	for _, role := range []struct {
		resolve func(context.Context) (*secret.Secret, error)
		proxy   bool
	}{
		{resolve: resolvers.target},
		{resolve: resolvers.proxy, proxy: true},
	} {
		if role.resolve == nil {
			continue
		}
		server, cancel, done, env, err := startAskpassServerEnvForRole(ctx, role.resolve, role.proxy)
		if err != nil {
			handle.cleanup()
			return nil, err
		}
		handle.servers = append(handle.servers, runningAskpassServer{server: server, cancel: cancel, done: done})
		handle.env = append(handle.env, env...)
	}
	return handle, nil
}

func controlCommandRequest(resolved *ResolvedHost, command string, sshArgs []string, cfg *config.Config, opts Options) connector.ControlCommandRequest {
	req := connector.ControlCommandRequest{
		Hostname:     resolved.Hostname,
		Username:     resolved.Username,
		Port:         resolved.Port,
		Command:      command,
		SSHOptions:   resolved.SSH,
		SSHVerbosity: opts.SSHVerbosity,
		SSHArgs:      sshArgs,
	}
	if cfg != nil && cfg.SSH.Connection.Timeout.Duration() > 0 {
		req.Timeout = int(cfg.SSH.Connection.Timeout.Duration().Seconds())
	}
	return req
}

func muxStartRequest(resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options, env []string) connector.MuxStartRequest {
	req := connector.MuxStartRequest{
		Hostname:     resolved.Hostname,
		Username:     resolved.Username,
		Port:         resolved.Port,
		SSHOptions:   resolved.SSH,
		SSHVerbosity: opts.SSHVerbosity,
		SSHArgs:      sshArgs,
		Env:          env,
	}
	if cfg != nil && cfg.SSH.Connection.Timeout.Duration() > 0 {
		req.Timeout = int(cfg.SSH.Connection.Timeout.Duration().Seconds())
	}
	return req
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
		if result, resolved, compatErr = handleCompatibilityFixes(ctx, hostname, explicitUser, false, resolved, sshArgs, cfg, audit, options, result); compatErr != nil {
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
	passwordFuture, muxHot := preparePasswordPrefetch(ctx, resolved, sshArgs, cfg, opts)
	if passwordFuture != nil {
		defer passwordFuture.Close()
	}
	proxyPasswordFuture := prepareProxyPasswordFuture(resolved)
	if proxyPasswordFuture != nil {
		defer proxyPasswordFuture.Close()
	}
	defer destroyImmediateProxyPassword(resolved)
	passwordResolvers := interactiveAskpassResolvers(resolved, passwordFuture, proxyPasswordFuture)
	if passwordResolvers.any() && passwordFuture == nil {
		muxHot, _ = checkMuxSessionFunc(ctx, muxCheckRequest(resolved, sshArgs, cfg, opts))
	}
	var askpassHandle *connectionAskpassHandle
	if interactiveAskpassEnabled() && passwordResolvers.any() && !muxHot {
		askpassTimer := connector.StartTiming(connector.TimingAskpassSetup)
		var err error
		askpassHandle, err = startConnectionAskpass(ctx, passwordResolvers)
		askpassTimer.Emit()
		if err != nil {
			return connectionResult{Err: err}
		}
		defer askpassHandle.cleanup()
	}
	var hostKeyPrep *connector.HostKeyPreparation
	if interactiveAskpassEnabled() && passwordResolvers.any() && !muxHot {
		var err error
		hostKeyPrep, err = prepareInteractiveHostKey(ctx, resolved, sshArgs, cfg, opts, proxyAskpassEnv(askpassHandle))
		if err != nil {
			return connectionResult{Err: err}
		}
		if hostKeyPrep != nil {
			defer hostKeyPrep.Cleanup()
			sshArgs = appendHostKeyPreparationSSHArgs(sshArgs, hostKeyPrep)
		}
	}
	conn := newConnector(resolved, sshArgs, cfg, opts)
	if askpassHandle != nil {
		conn.SetEnv(askpassHandle.env)
	}
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

func interactiveAskpassEnabled() bool {
	return true
}

func interactiveAskpassResolvers(resolved *ResolvedHost, passwordFuture, proxyPasswordFuture *sshpassword.Future) passwordResolvers {
	if resolved == nil {
		return passwordResolvers{}
	}
	resolvers := passwordResolvers{}
	if !hasUnmanagedProxyTransport(resolved) {
		resolvers.target = credentialPasswordResolver(resolved.AuthMode, resolved.Credential, passwordFuture)
	} else if resolved.Credential != nil {
		slog.Warn("password autofill disabled for unmanaged SSH proxy transport", "host", resolved.Hostname)
	}
	if resolved.Proxy != nil {
		resolvers.proxy = credentialPasswordResolver(resolved.Proxy.AuthMode, resolved.Proxy.Credential, proxyPasswordFuture)
	}
	return resolvers
}

func hasUnmanagedProxyTransport(resolved *ResolvedHost) bool {
	return resolved != nil && resolved.Proxy == nil &&
		(hasSSHOption(resolved.SSH.Options, "ProxyJump") || hasSSHOption(resolved.SSH.Options, "ProxyCommand"))
}

func credentialPasswordResolver(authMode string, credential *ResolvedCredential, future *sshpassword.Future) func(context.Context) (*secret.Secret, error) {
	if authMode == config.AuthModeKey || credential == nil {
		return nil
	}
	if future != nil {
		return future.Resolve
	}
	if credential.Password != nil {
		return func(context.Context) (*secret.Secret, error) { return credential.Password, nil }
	}
	if credential.PasswordResolver != nil {
		var once sync.Once
		var password *secret.Secret
		var err error
		return func(ctx context.Context) (*secret.Secret, error) {
			once.Do(func() { password, err = credential.PasswordResolver(ctx) })
			return password, err
		}
	}
	return nil
}

func prepareProxyPasswordFuture(resolved *ResolvedHost) *sshpassword.Future {
	if resolved == nil || resolved.Proxy == nil || resolved.Proxy.AuthMode == config.AuthModeKey || resolved.Proxy.Credential == nil || resolved.Proxy.Credential.PasswordResolver == nil {
		return nil
	}
	return sshpassword.NewFuture(resolved.Proxy.Credential.PasswordResolver)
}

func destroyImmediateProxyPassword(resolved *ResolvedHost) {
	if resolved != nil && resolved.Proxy != nil && resolved.Proxy.Credential != nil && resolved.Proxy.Credential.Password != nil {
		resolved.Proxy.Credential.Password.Destroy()
		resolved.Proxy.Credential.Password = nil
	}
}

func destroyImmediateResolvedPasswords(resolved *ResolvedHost) {
	if resolved != nil && resolved.Credential != nil && resolved.Credential.Password != nil {
		resolved.Credential.Password.Destroy()
		resolved.Credential.Password = nil
	}
	destroyImmediateProxyPassword(resolved)
}

func prepareInteractiveHostKey(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options, proxyEnv []string) (*connector.HostKeyPreparation, error) {
	switch hostKeyProbeFunc(ctx, resolved, sshArgs, cfg, opts, proxyEnv) {
	case hostKeyProbeNeedsPrompt:
		return hostKeyPrepareFunc(ctx, resolved, sshArgs, cfg, opts, false)
	case hostKeyProbeChanged:
		return hostKeyPrepareFunc(ctx, resolved, sshArgs, cfg, opts, true)
	default:
		return nil, nil
	}
}

func appendHostKeyPreparationSSHArgs(sshArgs []string, prep *connector.HostKeyPreparation) []string {
	if prep == nil {
		return sshArgs
	}
	prepArgs := prep.SSHArgs()
	if len(prepArgs) == 0 {
		return sshArgs
	}
	nextArgs := append([]string{}, sshArgs...)
	nextArgs = append(nextArgs, prepArgs...)
	return nextArgs
}

func handleCompatibilityFixes(ctx context.Context, query, explicitUser string, smart bool, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, audit *audit.Logger, opts Options, result connectionResult) (connectionResult, *ResolvedHost, error) {
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
		var nextResolved *ResolvedHost
		var err error
		if smart {
			nextResolved, err = ResolveSmartHostForConnect(query, explicitUser, cfg)
		} else {
			nextResolved, err = ResolveHostForConnect(query, explicitUser, cfg)
		}
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

	passwordFuture, muxHot := preparePasswordPrefetch(ctx, retryResolved, sshArgs, cfg, opts)
	if passwordFuture != nil {
		defer passwordFuture.Close()
	}
	proxyPasswordFuture := prepareProxyPasswordFuture(retryResolved)
	if proxyPasswordFuture != nil {
		defer proxyPasswordFuture.Close()
	}
	defer destroyImmediateProxyPassword(retryResolved)
	passwordResolvers := interactiveAskpassResolvers(retryResolved, passwordFuture, proxyPasswordFuture)
	if passwordResolvers.any() && passwordFuture == nil {
		muxHot, _ = checkMuxSessionFunc(ctx, muxCheckRequest(retryResolved, sshArgs, cfg, opts))
	}
	var askpassHandle *connectionAskpassHandle
	if interactiveAskpassEnabled() && passwordResolvers.any() && !muxHot {
		askpassTimer := connector.StartTiming(connector.TimingAskpassSetup)
		var err error
		askpassHandle, err = startConnectionAskpass(ctx, passwordResolvers)
		askpassTimer.Emit()
		if err != nil {
			return err
		}
		defer askpassHandle.cleanup()
	}
	var hostKeyPrep *connector.HostKeyPreparation
	if interactiveAskpassEnabled() && passwordResolvers.any() && !muxHot {
		var prepErr error
		hostKeyPrep, prepErr = prepareInteractiveHostKey(ctx, retryResolved, sshArgs, cfg, opts, proxyAskpassEnv(askpassHandle))
		if prepErr != nil {
			return prepErr
		}
		if hostKeyPrep != nil {
			defer hostKeyPrep.Cleanup()
			sshArgs = appendHostKeyPreparationSSHArgs(sshArgs, hostKeyPrep)
		}
	}
	conn := connector.NewConnector(resolved.Hostname, resolved.Username, retryPassword, sshArgs)
	if askpassHandle != nil {
		conn.SetEnv(askpassHandle.env)
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
