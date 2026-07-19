package connect

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/askpass"
	"github.com/ntwrknrd/nssh/internal/ssh/captured"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func TestHostNotFoundErrorCarriesHostname(t *testing.T) {
	var err error = &HostNotFoundError{Hostname: "edge01"}
	var notFound *HostNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatal("HostNotFoundError should support errors.As")
	}
	if notFound.Hostname != "edge01" || notFound.Error() != "host not found: edge01" {
		t.Fatalf("notFound = %+v error=%q", notFound, notFound.Error())
	}
}

func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout = stdoutWrite
	os.Stderr = stderrWrite
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	fn()

	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	stdout, err := io.ReadAll(stdoutRead)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	stderr, err := io.ReadAll(stderrRead)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(stdout), string(stderr)
}

func TestIsCompatibilityError(t *testing.T) {
	if !isCompatibilityError(&exit.ExitError{Code: exit.ExitConnectionFailed}) {
		t.Fatal("connection failed exit should be compatibility candidate")
	}
	if !isCompatibilityError(&exit.ExitError{Code: 255}) {
		t.Fatal("ssh exit 255 should be compatibility candidate")
	}
	if isCompatibilityError(&exit.ExitError{Code: exit.ExitAuthFailed}) {
		t.Fatal("auth failure should not be compatibility candidate")
	}
	if isCompatibilityError(errors.New("plain error")) {
		t.Fatal("plain error should not be compatibility candidate")
	}
}

func TestExtractExplicitUser(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		sshArgs  []string
		want     string
	}{
		{name: "user at host", hostname: "admin@edge01", want: "admin"},
		{name: "split login flag", hostname: "edge01", sshArgs: []string{"-l", "admin"}, want: "admin"},
		{name: "joined login flag", hostname: "edge01", sshArgs: []string{"-ladmin"}, want: "admin"},
		{name: "no explicit user", hostname: "edge01", sshArgs: []string{"-p", "2222"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractExplicitUser(tt.hostname, tt.sshArgs); got != tt.want {
				t.Fatalf("extractExplicitUser() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConnectRequestRoutesInteractiveSessionToPTYConnector(t *testing.T) {
	var interactiveCalled bool
	oldConnectHost := connectHostFunc
	oldRunRemoteCommand := runRemoteCommandFunc
	oldResolveHostname := resolveHostnameFunc
	defer func() {
		connectHostFunc = oldConnectHost
		runRemoteCommandFunc = oldRunRemoteCommand
		resolveHostnameFunc = oldResolveHostname
	}()

	resolveHostnameFunc = func(host string) (string, error) {
		t.Fatalf("ConnectRequest should not pre-resolve non-literal interactive host, got %q", host)
		return "", nil
	}
	connectHostFunc = func(_ context.Context, host string, sshArgs []string, _ ...Options) error {
		interactiveCalled = true
		if host != "edge01" {
			t.Fatalf("interactive host = %q, want raw host", host)
		}
		if len(sshArgs) != 2 || sshArgs[0] != "-p" || sshArgs[1] != "2222" {
			t.Fatalf("interactive ssh args = %#v", sshArgs)
		}
		return nil
	}
	runRemoteCommandFunc = func(_ context.Context, _ string, _ []string, _ []string, _ ...Options) error {
		t.Fatal("remote command runner should not be called for interactive sessions")
		return nil
	}

	err := ConnectRequest(context.Background(), Request{
		Host:    "edge01",
		SSHArgs: []string{"-p", "2222"},
	})
	if err != nil {
		t.Fatalf("ConnectRequest: %v", err)
	}
	if !interactiveCalled {
		t.Fatal("interactive connector was not called")
	}
}

func TestConnectRequestRoutesRemoteCommandToCapturedRunner(t *testing.T) {
	var remoteCalled bool
	oldConnectHost := connectHostFunc
	oldRunRemoteCommand := runRemoteCommandFunc
	oldResolveHostname := resolveHostnameFunc
	defer func() {
		connectHostFunc = oldConnectHost
		runRemoteCommandFunc = oldRunRemoteCommand
		resolveHostnameFunc = oldResolveHostname
	}()

	resolveHostnameFunc = func(host string) (string, error) {
		t.Fatalf("ConnectRequest should not pre-resolve non-literal remote host, got %q", host)
		return "", nil
	}
	connectHostFunc = func(_ context.Context, _ string, _ []string, _ ...Options) error {
		t.Fatal("interactive connector should not be called for remote commands")
		return nil
	}
	runRemoteCommandFunc = func(_ context.Context, host string, sshArgs, command []string, _ ...Options) error {
		remoteCalled = true
		if host != "edge01" {
			t.Fatalf("remote host = %q, want raw host", host)
		}
		if len(sshArgs) != 2 || sshArgs[0] != "-p" || sshArgs[1] != "2222" {
			t.Fatalf("remote ssh args = %#v", sshArgs)
		}
		if len(command) != 2 || command[0] != "show" || command[1] != "version" {
			t.Fatalf("remote command = %#v", command)
		}
		return nil
	}

	err := ConnectRequest(context.Background(), Request{
		Host:          "edge01",
		SSHArgs:       []string{"-p", "2222"},
		RemoteCommand: []string{"show", "version"},
	})
	if err != nil {
		t.Fatalf("ConnectRequest: %v", err)
	}
	if !remoteCalled {
		t.Fatal("remote command runner was not called")
	}
}

func TestConnectRequestRoutesControlCommandToNonPTYRunner(t *testing.T) {
	var controlCalled bool
	oldConnectHost := connectHostFunc
	oldRunControlCommand := runControlCommandFunc
	oldResolveHostname := resolveHostnameFunc
	defer func() {
		connectHostFunc = oldConnectHost
		runControlCommandFunc = oldRunControlCommand
		resolveHostnameFunc = oldResolveHostname
	}()

	resolveHostnameFunc = func(host string) (string, error) {
		t.Fatalf("ConnectRequest should not pre-resolve non-literal control host, got %q", host)
		return "", nil
	}
	connectHostFunc = func(_ context.Context, _ string, _ []string, _ ...Options) error {
		t.Fatal("interactive connector should not be called for control commands")
		return nil
	}
	runControlCommandFunc = func(_ context.Context, host string, literal bool, sshArgs []string, _ ...Options) error {
		controlCalled = true
		if host != "edge01" {
			t.Fatalf("control host = %q, want raw host", host)
		}
		if literal {
			t.Fatal("literal target should be false")
		}
		if len(sshArgs) != 2 || sshArgs[0] != "-O" || sshArgs[1] != "exit" {
			t.Fatalf("control ssh args = %#v", sshArgs)
		}
		return nil
	}

	err := ConnectRequest(context.Background(), Request{
		Host:    "edge01",
		SSHArgs: []string{"-O", "exit"},
	})
	if err != nil {
		t.Fatalf("ConnectRequest: %v", err)
	}
	if !controlCalled {
		t.Fatal("control command runner was not called")
	}
}

func TestConnectRequestPreservesLiteralTargetForRemoteCommand(t *testing.T) {
	var remoteCalled bool
	oldRunLiteralRemoteCommand := runLiteralRemoteCommandFunc
	oldResolveHostname := resolveHostnameFunc
	defer func() {
		runLiteralRemoteCommandFunc = oldRunLiteralRemoteCommand
		resolveHostnameFunc = oldResolveHostname
	}()

	resolveHostnameFunc = func(host string) (string, error) {
		t.Fatalf("literal target should not resolve hostname, got %q", host)
		return "", nil
	}
	runLiteralRemoteCommandFunc = func(_ context.Context, host string, sshArgs, command []string, _ ...Options) error {
		remoteCalled = true
		if host != "log" {
			t.Fatalf("literal host = %q, want log", host)
		}
		if len(sshArgs) != 2 || sshArgs[0] != "-p" || sshArgs[1] != "2222" {
			t.Fatalf("literal ssh args = %#v", sshArgs)
		}
		if len(command) != 2 || command[0] != "show" || command[1] != "version" {
			t.Fatalf("literal command = %#v", command)
		}
		return nil
	}

	err := ConnectRequest(context.Background(), Request{
		Host:          "log",
		LiteralTarget: true,
		SSHArgs:       []string{"-p", "2222"},
		RemoteCommand: []string{"show", "version"},
	})
	if err != nil {
		t.Fatalf("ConnectRequest: %v", err)
	}
	if !remoteCalled {
		t.Fatal("literal remote command runner was not called")
	}
}

func TestRunResolvedRemoteCommandHighlightsStdoutOnly(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	defer func() { runCapturedCommandFunc = oldRunCapturedCommand }()

	highlightEnabled := true
	var gotReq captured.Request
	runCapturedCommandFunc = func(_ context.Context, req captured.Request) (captured.Result, error) {
		gotReq = req
		return captured.Result{
			Stdout: []byte("set protocols bgp\n"),
			Stderr: []byte("set protocols bgp\n"),
		}, nil
	}

	stdout, stderr := captureOutput(t, func() {
		err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
			Hostname:  "edge01",
			Username:  "netops",
			Port:      2200,
			Highlight: config.HighlightConfig{Enabled: &highlightEnabled, Profile: config.HighlightProfileJunos},
		}, []string{"-o", "LogLevel=ERROR"}, []string{"show", "configuration"}, config.DefaultConfig(), Options{})
		if err != nil {
			t.Fatalf("runResolvedRemoteCommand: %v", err)
		}
	})

	if gotReq.Hostname != "edge01" || gotReq.Username != "netops" || gotReq.Port != 2200 {
		t.Fatalf("captured request endpoint = %+v", gotReq)
	}
	if len(gotReq.SSHArgs) != 2 || gotReq.SSHArgs[0] != "-o" || gotReq.SSHArgs[1] != "LogLevel=ERROR" {
		t.Fatalf("captured request ssh args = %#v", gotReq.SSHArgs)
	}
	if len(gotReq.RemoteCommand) != 2 || gotReq.RemoteCommand[0] != "show" || gotReq.RemoteCommand[1] != "configuration" {
		t.Fatalf("captured request command = %#v", gotReq.RemoteCommand)
	}
	if !bytes.Contains([]byte(stdout), []byte("\x1b[")) {
		t.Fatalf("stdout was not highlighted: %q", stdout)
	}
	if stderr != "set protocols bgp\n" {
		t.Fatalf("stderr = %q, want raw unhighlighted text", stderr)
	}
}

func TestRunResolvedRemoteCommandPreservesCapturedOutputOrder(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	defer func() { runCapturedCommandFunc = oldRunCapturedCommand }()

	highlightEnabled := true
	runCapturedCommandFunc = func(_ context.Context, _ captured.Request) (captured.Result, error) {
		return captured.Result{
			Stdout: []byte("set protocols bgp\n"),
			Stderr: []byte("banner\n"),
			Output: []captured.OutputEvent{
				{Stream: captured.StreamStderr, Data: []byte("banner\n")},
				{Stream: captured.StreamStdout, Data: []byte("set protocols bgp\n")},
			},
		}, nil
	}

	stdout, stderr := captureOutput(t, func() {
		err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
			Hostname:  "edge01",
			Highlight: config.HighlightConfig{Enabled: &highlightEnabled, Profile: config.HighlightProfileJunos},
		}, nil, []string{"show"}, config.DefaultConfig(), Options{})
		if err != nil {
			t.Fatalf("runResolvedRemoteCommand: %v", err)
		}
	})

	if stdout == "" || stderr != "banner\n" {
		t.Fatalf("stdout=%q stderr=%q, want highlighted stdout and raw stderr", stdout, stderr)
	}
	if !bytes.Contains([]byte(stdout), []byte("\x1b[")) {
		t.Fatalf("stdout was not highlighted: %q", stdout)
	}
}

func TestRunResolvedRemoteCommandLeavesDisabledHighlightUnchanged(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	defer func() { runCapturedCommandFunc = oldRunCapturedCommand }()

	runCapturedCommandFunc = func(_ context.Context, _ captured.Request) (captured.Result, error) {
		return captured.Result{Stdout: []byte("set protocols bgp\n")}, nil
	}

	stdout, _ := captureOutput(t, func() {
		err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
			Hostname: "edge01",
			Port:     22,
		}, nil, []string{"show"}, config.DefaultConfig(), Options{})
		if err != nil {
			t.Fatalf("runResolvedRemoteCommand: %v", err)
		}
	})

	if stdout != "set protocols bgp\n" {
		t.Fatalf("stdout = %q, want unchanged", stdout)
	}
}

func TestRunResolvedRemoteCommandRequiresAskpassHelperForPassword(t *testing.T) {
	oldAskpassHelperPath := askpassHelperPathFunc
	defer func() { askpassHelperPathFunc = oldAskpassHelperPath }()

	askpassHelperPathFunc = func() (string, error) {
		return "", errors.New("nssh-askpass not found")
	}
	password := secret.NewFromString("super-secret")
	defer password.Destroy()

	err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
		Hostname: "edge01",
		Port:     22,
		Credential: &ResolvedCredential{
			Password: password,
		},
	}, nil, []string{"show"}, config.DefaultConfig(), Options{})
	if err == nil || !strings.Contains(err.Error(), "nssh-askpass not found") {
		t.Fatalf("err = %v, want missing askpass helper", err)
	}
}

func TestStartAskpassServerEnvUsesHelperEnvironment(t *testing.T) {
	oldAskpassHelperPath := askpassHelperPathFunc
	defer func() { askpassHelperPathFunc = oldAskpassHelperPath }()
	askpassHelperPathFunc = func() (string, error) {
		return "/tmp/nssh-askpass", nil
	}

	server, cancel, done, env, err := startAskpassServerEnv(context.Background(), func(context.Context) (*secret.Secret, error) {
		return secret.NewFromString("secret"), nil
	})
	if err != nil {
		t.Fatalf("startAskpassServerEnv: %v", err)
	}
	stopAskpassServer(server, cancel, done)

	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"SSH_ASKPASS=/tmp/nssh-askpass",
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=nssh-askpass",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("env = %q, want %q", joined, want)
		}
	}
}

func TestStartAskpassServerEnvSupportsMultiplePrompts(t *testing.T) {
	oldAskpassHelperPath := askpassHelperPathFunc
	defer func() { askpassHelperPathFunc = oldAskpassHelperPath }()
	askpassHelperPathFunc = func() (string, error) {
		return "/tmp/nssh-askpass", nil
	}

	password := secret.NewFromString("secret")
	defer password.Destroy()
	server, cancel, done, env, err := startAskpassServerEnv(context.Background(), func(context.Context) (*secret.Secret, error) {
		return password, nil
	})
	if err != nil {
		t.Fatalf("startAskpassServerEnv: %v", err)
	}
	defer stopAskpassServer(server, cancel, done)

	socketPath := envValue(env, askpass.SocketEnv)
	nonce := envValue(env, askpass.NonceEnv)
	if socketPath == "" || nonce == "" {
		t.Fatalf("askpass env missing socket or nonce: %#v", env)
	}

	for i := 0; i < 2; i++ {
		got, err := askpass.RequestPassword(context.Background(), socketPath, nonce)
		if err != nil {
			t.Fatalf("RequestPassword %d: %v", i+1, err)
		}
		if string(got) != "secret" {
			t.Fatalf("password %d = %q", i+1, got)
		}
	}
}

func TestCapturedManagedProxyAskpassKeepsSecretsOutOfTransportAndLogs(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	oldAskpassHelperPath := askpassHelperPathFunc
	oldCheckMuxSession := checkMuxSessionFunc
	oldHostKeyProbe := hostKeyProbeFunc
	oldLogger := slog.Default()
	defer func() {
		runCapturedCommandFunc = oldRunCapturedCommand
		askpassHelperPathFunc = oldAskpassHelperPath
		checkMuxSessionFunc = oldCheckMuxSession
		hostKeyProbeFunc = oldHostKeyProbe
		slog.SetDefault(oldLogger)
	}()

	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	checkMuxSessionFunc = func(context.Context, connector.MuxCheckRequest) (bool, bool) { return false, false }
	hostKeyProbeFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options) hostKeyProbeStatus {
		return hostKeyProbeClean
	}
	askpassHelperPathFunc = func() (string, error) { return "/tmp/nssh-askpass", nil }
	targetPassword := secret.NewFromString("target-transport-sentinel")
	proxyPassword := secret.NewFromString("proxy-transport-sentinel")

	runCapturedCommandFunc = func(ctx context.Context, req captured.Request) (captured.Result, error) {
		transport := strings.Join(req.Env, "\n") + strings.Join(req.SSHArgs, "\n") + strings.Join(req.RemoteCommand, "\n")
		for key, value := range req.SSHOptions.Options {
			transport += "\n" + key + "=" + value.StringValue()
		}
		for _, sentinel := range []string{"target-transport-sentinel", "proxy-transport-sentinel"} {
			if strings.Contains(transport, sentinel) {
				t.Fatalf("captured SSH transport contains secret sentinel %q", sentinel)
			}
		}
		targetSocket := envValue(req.Env, askpass.SocketEnv)
		targetNonce := envValue(req.Env, askpass.NonceEnv)
		proxySocket := envValue(req.Env, askpass.ProxySocketEnv)
		proxyNonce := envValue(req.Env, askpass.ProxyNonceEnv)
		gotProxy, err := askpass.RequestPassword(ctx, proxySocket, proxyNonce)
		if err != nil {
			return captured.Result{}, err
		}
		gotTarget, err := askpass.RequestPassword(ctx, targetSocket, targetNonce)
		if err != nil {
			return captured.Result{}, err
		}
		if string(gotProxy) != "proxy-transport-sentinel" || string(gotTarget) != "target-transport-sentinel" {
			t.Fatal("captured askpass returned the wrong per-hop credential")
		}
		return captured.Result{Stdout: []byte("ok\n")}, nil
	}

	err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
		Hostname: "edge01.example",
		Username: "target-user",
		Port:     22,
		AuthMode: config.AuthModePassword,
		SSH: config.SSHHostConfig{Options: config.SSHOptions{
			"ProxyCommand": config.NewSSHOptionString(managedProxyAskpassPrefix + "ssh -W %h:%p proxy-user@jump01.example"),
		}},
		Credential: &ResolvedCredential{Password: targetPassword},
		Proxy: &ResolvedProxy{
			Canonical:  "jump01.example",
			Hostname:   "jump01.example",
			Username:   "proxy-user",
			AuthMode:   config.AuthModePassword,
			Credential: &ResolvedCredential{Password: proxyPassword},
		},
	}, nil, []string{"show", "version"}, config.DefaultConfig(), Options{})
	if err != nil {
		t.Fatalf("runResolvedRemoteCommand: %v", err)
	}
	for _, sentinel := range []string{"target-transport-sentinel", "proxy-transport-sentinel"} {
		if strings.Contains(logs.String(), sentinel) {
			t.Fatalf("debug logs contain secret sentinel %q", sentinel)
		}
	}
}

func TestCapturedProxyCredentialRunsHostKeyPreparationWithoutTargetCredential(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	oldAskpassHelperPath := askpassHelperPathFunc
	oldCheckMuxSession := checkMuxSessionFunc
	oldHostKeyProbe := hostKeyProbeFunc
	defer func() {
		runCapturedCommandFunc = oldRunCapturedCommand
		askpassHelperPathFunc = oldAskpassHelperPath
		checkMuxSessionFunc = oldCheckMuxSession
		hostKeyProbeFunc = oldHostKeyProbe
	}()

	var probeCalls atomic.Int32
	checkMuxSessionFunc = func(context.Context, connector.MuxCheckRequest) (bool, bool) { return false, false }
	hostKeyProbeFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options) hostKeyProbeStatus {
		probeCalls.Add(1)
		return hostKeyProbeClean
	}
	askpassHelperPathFunc = func() (string, error) { return "/tmp/nssh-askpass", nil }
	runCapturedCommandFunc = func(_ context.Context, req captured.Request) (captured.Result, error) {
		if envValue(req.Env, askpass.SocketEnv) != "" {
			t.Fatal("key-auth target unexpectedly received a target askpass channel")
		}
		if envValue(req.Env, askpass.ProxySocketEnv) == "" {
			t.Fatal("password-auth proxy did not receive its isolated askpass channel")
		}
		return captured.Result{}, nil
	}

	proxyPassword := secret.NewFromString("proxy-secret")
	err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
		Hostname: "edge01.example",
		AuthMode: config.AuthModeKey,
		SSH: config.SSHHostConfig{Options: config.SSHOptions{
			"ProxyCommand": config.NewSSHOptionString(managedProxyAskpassPrefix + "ssh -W %h:%p proxy-user@jump01.example"),
		}},
		Proxy: &ResolvedProxy{
			Hostname:   "jump01.example",
			AuthMode:   config.AuthModePassword,
			Credential: &ResolvedCredential{Password: proxyPassword},
		},
	}, nil, []string{"show"}, config.DefaultConfig(), Options{})
	if err != nil {
		t.Fatalf("runResolvedRemoteCommand: %v", err)
	}
	if got := probeCalls.Load(); got != 1 {
		t.Fatalf("host-key probe calls = %d, want 1", got)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func TestInteractiveAskpassEnabledByDefault(t *testing.T) {
	t.Setenv("NSSH_EXPERIMENT_INTERACTIVE_ASKPASS", "")
	if !interactiveAskpassEnabled() {
		t.Fatal("interactive askpass disabled by default")
	}
}

func TestInteractiveAskpassResolversUseImmediateTargetPassword(t *testing.T) {
	password := secret.NewFromString("secret")
	resolved := &ResolvedHost{
		AuthMode: config.AuthModePassword,
		Credential: &ResolvedCredential{
			Password: password,
		},
	}

	resolvers := interactiveAskpassResolvers(resolved, nil, nil)
	if resolvers.target == nil {
		t.Fatal("interactive askpass resolver is nil")
	}
	got, err := resolvers.target(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != password {
		t.Fatal("resolver did not return immediate password")
	}
}

func TestInteractiveAskpassResolversDoNotCreateProxyChannelForKeyAuth(t *testing.T) {
	targetPassword := secret.NewFromString("target-secret")
	defer targetPassword.Destroy()
	resolved := &ResolvedHost{
		Hostname:   "edge01.example",
		AuthMode:   config.AuthModePassword,
		Credential: &ResolvedCredential{Password: targetPassword},
		Proxy: &ResolvedProxy{
			Canonical: "jump01.example",
			Hostname:  "jump01.example",
			AuthMode:  config.AuthModeKey,
		},
	}

	resolvers := interactiveAskpassResolvers(resolved, nil, nil)
	if resolvers.proxy != nil {
		t.Fatal("key-auth proxy unexpectedly received an askpass channel")
	}
	got, err := resolvers.target(context.Background())
	if err != nil || got != targetPassword {
		t.Fatalf("ambiguous target prompt resolved password=%v err=%v", got != nil, err)
	}
}

func TestInteractiveAskpassResolversDisableTargetChannelForUnmanagedProxyTransport(t *testing.T) {
	for _, option := range []string{"ProxyJump", "ProxyCommand"} {
		t.Run(option, func(t *testing.T) {
			password := secret.NewFromString("target-secret")
			defer password.Destroy()
			resolved := &ResolvedHost{
				Hostname:   "edge01.example",
				AuthMode:   config.AuthModePassword,
				Credential: &ResolvedCredential{Password: password},
				SSH: config.SSHHostConfig{Options: config.SSHOptions{
					option: config.NewSSHOptionString("unmanaged-proxy"),
				}},
			}

			resolvers := interactiveAskpassResolvers(resolved, nil, nil)
			if resolvers.target != nil || resolvers.proxy != nil {
				t.Fatalf("unmanaged %s received askpass resolvers", option)
			}
			if shouldPrefetchPassword(resolved) {
				t.Fatalf("unmanaged %s enabled password prefetch", option)
			}
		})
	}
}

func TestRunResolvedRemoteCommandHotMuxSkipsAskpassAndPasswordResolver(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	oldAskpassHelperPath := askpassHelperPathFunc
	oldCheckMuxSession := checkMuxSessionFunc
	oldHostKeyProbe := hostKeyProbeFunc
	defer func() {
		runCapturedCommandFunc = oldRunCapturedCommand
		askpassHelperPathFunc = oldAskpassHelperPath
		checkMuxSessionFunc = oldCheckMuxSession
		hostKeyProbeFunc = oldHostKeyProbe
	}()

	var resolverCalls atomic.Int32
	checkMuxSessionFunc = func(_ context.Context, req connector.MuxCheckRequest) (bool, bool) {
		if req.Hostname != "edge01" || req.Username != "netops" || req.Port != 2200 {
			t.Fatalf("mux request = %+v, want resolved endpoint", req)
		}
		return true, true
	}
	askpassHelperPathFunc = func() (string, error) {
		t.Fatal("hot mux should not require askpass helper")
		return "", nil
	}
	startMuxSessionFunc = func(context.Context, connector.MuxStartRequest) error {
		t.Fatal("hot mux should not start a new mux session")
		return nil
	}
	runCapturedCommandFunc = func(_ context.Context, req captured.Request) (captured.Result, error) {
		if len(req.Env) != 0 {
			t.Fatalf("captured env = %#v, want no askpass env", req.Env)
		}
		return captured.Result{Stdout: []byte("ok\n")}, nil
	}

	err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
		Hostname: "edge01",
		Username: "netops",
		Port:     2200,
		AuthMode: config.AuthModePassword,
		SSH: config.SSHHostConfig{Options: config.SSHOptions{
			"ControlPath": config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
		}},
		Credential: &ResolvedCredential{
			PasswordResolver: func(context.Context) (*secret.Secret, error) {
				resolverCalls.Add(1)
				return secret.NewFromString("secret"), nil
			},
		},
	}, nil, []string{"show"}, config.DefaultConfig(), Options{})
	if err != nil {
		t.Fatalf("runResolvedRemoteCommand: %v", err)
	}
	if got := resolverCalls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
}

func TestRunResolvedRemoteCommandColdPersistentMuxStartsMuxBeforeCapturedCommand(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	oldAskpassHelperPath := askpassHelperPathFunc
	oldCheckMuxSession := checkMuxSessionFunc
	oldStartMuxSession := startMuxSessionFunc
	oldHostKeyProbe := hostKeyProbeFunc
	oldHostKeyPrepare := hostKeyPrepareFunc
	defer func() {
		runCapturedCommandFunc = oldRunCapturedCommand
		askpassHelperPathFunc = oldAskpassHelperPath
		checkMuxSessionFunc = oldCheckMuxSession
		startMuxSessionFunc = oldStartMuxSession
		hostKeyProbeFunc = oldHostKeyProbe
		hostKeyPrepareFunc = oldHostKeyPrepare
	}()

	checkMuxSessionFunc = func(context.Context, connector.MuxCheckRequest) (bool, bool) {
		return false, true
	}
	hostKeyProbeFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options) hostKeyProbeStatus {
		return hostKeyProbeNeedsPrompt
	}
	hostKeyPrepareFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, bool) (*connector.HostKeyPreparation, error) {
		return &connector.HostKeyPreparation{TempKnownHosts: "/tmp/nssh-known-hosts-prep"}, nil
	}
	askpassHelperPathFunc = func() (string, error) {
		return "/tmp/nssh-askpass", nil
	}
	var muxStarted atomic.Bool
	var targetResolverCalls atomic.Int32
	var proxyResolverCalls atomic.Int32
	startMuxSessionFunc = func(_ context.Context, req connector.MuxStartRequest) error {
		muxStarted.Store(true)
		if req.Hostname != "edge01" || req.Username != "netops" || req.Port != 2200 {
			t.Fatalf("mux start request endpoint = %+v", req)
		}
		if len(req.Env) == 0 {
			t.Fatal("mux start request missing askpass env")
		}
		transport := strings.Join(req.Env, "\n") + strings.Join(req.SSHArgs, "\n")
		for key, value := range req.SSHOptions.Options {
			transport += "\n" + key + "=" + value.StringValue()
		}
		for _, sentinel := range []string{"target-secret", "proxy-secret"} {
			if strings.Contains(transport, sentinel) {
				t.Fatalf("mux transport contains secret sentinel %q", sentinel)
			}
		}
		joinedArgs := strings.Join(req.SSHArgs, "\n")
		for _, want := range []string{
			"UserKnownHostsFile=/tmp/nssh-known-hosts-prep",
			"StrictHostKeyChecking=yes",
		} {
			if !strings.Contains(joinedArgs, want) {
				t.Fatalf("mux start ssh args = %#v, want %q", req.SSHArgs, want)
			}
		}
		targetSocket := envValue(req.Env, askpass.SocketEnv)
		targetNonce := envValue(req.Env, askpass.NonceEnv)
		proxySocket := envValue(req.Env, askpass.ProxySocketEnv)
		proxyNonce := envValue(req.Env, askpass.ProxyNonceEnv)
		gotProxy, err := askpass.RequestPassword(context.Background(), proxySocket, proxyNonce)
		if err != nil {
			t.Fatalf("request proxy password: %v", err)
		}
		gotTarget, err := askpass.RequestPassword(context.Background(), targetSocket, targetNonce)
		if err != nil {
			t.Fatalf("request target password: %v", err)
		}
		if string(gotProxy) != "proxy-secret" || string(gotTarget) != "target-secret" {
			t.Fatal("mux askpass routed a prompt to the wrong credential")
		}
		return nil
	}
	runCapturedCommandFunc = func(_ context.Context, req captured.Request) (captured.Result, error) {
		if !muxStarted.Load() {
			t.Fatal("captured command started before mux prewarm")
		}
		if len(req.Env) != 0 {
			t.Fatalf("captured env = %#v, want no askpass after mux prewarm", req.Env)
		}
		return captured.Result{Stdout: []byte("ok\n")}, nil
	}

	err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
		Hostname: "edge01",
		Username: "netops",
		Port:     2200,
		AuthMode: config.AuthModePassword,
		SSH: config.SSHHostConfig{Options: config.SSHOptions{
			"ControlMaster":  config.NewSSHOptionString("auto"),
			"ControlPath":    config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
			"ControlPersist": config.NewSSHOptionString("12h"),
			"ProxyCommand":   config.NewSSHOptionString(managedProxyAskpassPrefix + "ssh -W %h:%p proxy-user@jump01.example"),
		}},
		Credential: &ResolvedCredential{
			PasswordResolver: func(context.Context) (*secret.Secret, error) {
				targetResolverCalls.Add(1)
				return secret.NewFromString("target-secret"), nil
			},
		},
		Proxy: &ResolvedProxy{
			Canonical: "jump01.example",
			Hostname:  "jump01.example",
			Username:  "proxy-user",
			AuthMode:  config.AuthModePassword,
			Credential: &ResolvedCredential{PasswordResolver: func(context.Context) (*secret.Secret, error) {
				proxyResolverCalls.Add(1)
				return secret.NewFromString("proxy-secret"), nil
			}},
		},
	}, []string{"-o", "LogLevel=ERROR"}, []string{"show"}, config.DefaultConfig(), Options{})
	if err != nil {
		t.Fatalf("runResolvedRemoteCommand: %v", err)
	}
	if !muxStarted.Load() {
		t.Fatal("mux prewarm was not started")
	}
	if got := targetResolverCalls.Load(); got != 1 {
		t.Fatalf("target resolver calls = %d, want 1", got)
	}
	if got := proxyResolverCalls.Load(); got != 1 {
		t.Fatalf("proxy resolver calls = %d, want 1", got)
	}
}

func TestRunResolvedRemoteCommandSkipsMuxPrewarmWithoutControlPersist(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	oldCheckMuxSession := checkMuxSessionFunc
	oldStartMuxSession := startMuxSessionFunc
	defer func() {
		runCapturedCommandFunc = oldRunCapturedCommand
		checkMuxSessionFunc = oldCheckMuxSession
		startMuxSessionFunc = oldStartMuxSession
	}()

	checkMuxSessionFunc = func(context.Context, connector.MuxCheckRequest) (bool, bool) {
		return false, true
	}
	startMuxSessionFunc = func(context.Context, connector.MuxStartRequest) error {
		t.Fatal("mux prewarm should require ControlPersist")
		return nil
	}
	runCapturedCommandFunc = func(context.Context, captured.Request) (captured.Result, error) {
		return captured.Result{Stdout: []byte("ok\n")}, nil
	}

	err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
		Hostname: "edge01",
		Port:     22,
		SSH: config.SSHHostConfig{Options: config.SSHOptions{
			"ControlMaster": config.NewSSHOptionString("auto"),
			"ControlPath":   config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
		}},
	}, nil, []string{"show"}, config.DefaultConfig(), Options{})
	if err != nil {
		t.Fatalf("runResolvedRemoteCommand: %v", err)
	}
}

func TestRunResolvedRemoteCommandColdMuxStartsPrefetchWithoutWaiting(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	oldAskpassHelperPath := askpassHelperPathFunc
	oldCheckMuxSession := checkMuxSessionFunc
	oldStartMuxSession := startMuxSessionFunc
	oldHostKeyProbe := hostKeyProbeFunc
	defer func() {
		runCapturedCommandFunc = oldRunCapturedCommand
		askpassHelperPathFunc = oldAskpassHelperPath
		checkMuxSessionFunc = oldCheckMuxSession
		startMuxSessionFunc = oldStartMuxSession
		hostKeyProbeFunc = oldHostKeyProbe
	}()

	checkMuxSessionFunc = func(context.Context, connector.MuxCheckRequest) (bool, bool) {
		return false, true
	}
	hostKeyProbeFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options) hostKeyProbeStatus {
		return hostKeyProbeClean
	}
	startMuxSessionFunc = func(context.Context, connector.MuxStartRequest) error { return nil }
	askpassHelperPathFunc = func() (string, error) {
		return "/tmp/nssh-askpass", nil
	}
	resolverStarted := make(chan struct{})
	capturedCalled := make(chan captured.Request, 1)
	runCapturedCommandFunc = func(_ context.Context, req captured.Request) (captured.Result, error) {
		capturedCalled <- req
		return captured.Result{Stdout: []byte("ok\n")}, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- runResolvedRemoteCommand(context.Background(), &ResolvedHost{
			Hostname: "edge01",
			Port:     22,
			AuthMode: config.AuthModePassword,
			SSH: config.SSHHostConfig{Options: config.SSHOptions{
				"ControlPath": config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
			}},
			Credential: &ResolvedCredential{
				PasswordResolver: func(ctx context.Context) (*secret.Secret, error) {
					close(resolverStarted)
					<-ctx.Done()
					return nil, ctx.Err()
				},
			},
		}, nil, []string{"show"}, config.DefaultConfig(), Options{})
	}()

	select {
	case <-resolverStarted:
	case <-time.After(time.Second):
		t.Fatal("password prefetch did not start")
	}
	select {
	case req := <-capturedCalled:
		if len(req.Env) == 0 {
			t.Fatal("captured request missing askpass env")
		}
	case <-time.After(time.Second):
		t.Fatal("remote command waited for password resolver before spawning ssh")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runResolvedRemoteCommand: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runResolvedRemoteCommand did not finish")
	}
}

func TestRunResolvedRemoteCommandPreparesHostKeyBeforeAskpass(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	oldAskpassHelperPath := askpassHelperPathFunc
	oldCheckMuxSession := checkMuxSessionFunc
	oldHostKeyProbe := hostKeyProbeFunc
	oldHostKeyPrepare := hostKeyPrepareFunc
	defer func() {
		runCapturedCommandFunc = oldRunCapturedCommand
		askpassHelperPathFunc = oldAskpassHelperPath
		checkMuxSessionFunc = oldCheckMuxSession
		hostKeyProbeFunc = oldHostKeyProbe
		hostKeyPrepareFunc = oldHostKeyPrepare
	}()

	checkMuxSessionFunc = func(context.Context, connector.MuxCheckRequest) (bool, bool) {
		return false, true
	}
	hostKeyProbeFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options) hostKeyProbeStatus {
		return hostKeyProbeNeedsPrompt
	}
	var prepareCalled atomic.Bool
	hostKeyPrepareFunc = func(_ context.Context, _ *ResolvedHost, _ []string, _ *config.Config, _ Options, changed bool) (*connector.HostKeyPreparation, error) {
		if changed {
			t.Fatal("unknown host-key preparation was marked changed")
		}
		prepareCalled.Store(true)
		return &connector.HostKeyPreparation{TempKnownHosts: "/tmp/nssh-known-hosts-prep"}, nil
	}
	askpassHelperPathFunc = func() (string, error) {
		return "/tmp/nssh-askpass", nil
	}
	runCapturedCommandFunc = func(_ context.Context, req captured.Request) (captured.Result, error) {
		if !prepareCalled.Load() {
			t.Fatal("captured command started before host-key preparation")
		}
		if len(req.Env) == 0 {
			t.Fatal("captured request missing askpass env")
		}
		joinedArgs := strings.Join(req.SSHArgs, "\n")
		for _, want := range []string{
			"UserKnownHostsFile=/tmp/nssh-known-hosts-prep",
			"StrictHostKeyChecking=yes",
		} {
			if !strings.Contains(joinedArgs, want) {
				t.Fatalf("ssh args = %#v, want %q", req.SSHArgs, want)
			}
		}
		return captured.Result{Stdout: []byte("ok\n")}, nil
	}

	err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
		Hostname: "edge01",
		Port:     22,
		AuthMode: config.AuthModePassword,
		Credential: &ResolvedCredential{
			PasswordResolver: func(context.Context) (*secret.Secret, error) {
				return secret.NewFromString("secret"), nil
			},
		},
	}, nil, []string{"show"}, config.DefaultConfig(), Options{})
	if err != nil {
		t.Fatalf("runResolvedRemoteCommand: %v", err)
	}
	if !prepareCalled.Load() {
		t.Fatal("host-key preparation was not called")
	}
}

func TestPrepareInteractiveHostKeyPassesChangedStatus(t *testing.T) {
	oldHostKeyProbe := hostKeyProbeFunc
	oldHostKeyPrepare := hostKeyPrepareFunc
	defer func() {
		hostKeyProbeFunc = oldHostKeyProbe
		hostKeyPrepareFunc = oldHostKeyPrepare
	}()

	hostKeyProbeFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options) hostKeyProbeStatus {
		return hostKeyProbeChanged
	}
	var sawChanged bool
	hostKeyPrepareFunc = func(_ context.Context, _ *ResolvedHost, _ []string, _ *config.Config, _ Options, changed bool) (*connector.HostKeyPreparation, error) {
		sawChanged = changed
		return nil, nil
	}

	_, err := prepareInteractiveHostKey(context.Background(), &ResolvedHost{Hostname: "edge01"}, nil, config.DefaultConfig(), Options{})
	if err != nil {
		t.Fatalf("prepareInteractiveHostKey: %v", err)
	}
	if !sawChanged {
		t.Fatal("changed host-key probe did not pass changed=true to preparation")
	}
}

func TestPreparePasswordPrefetchHotMuxPreservesLazyFallback(t *testing.T) {
	oldCheckMuxSession := checkMuxSessionFunc
	defer func() { checkMuxSessionFunc = oldCheckMuxSession }()

	checkMuxSessionFunc = func(context.Context, connector.MuxCheckRequest) (bool, bool) {
		return true, true
	}
	var resolverCalls atomic.Int32
	future, muxHot := preparePasswordPrefetch(context.Background(), &ResolvedHost{
		Hostname: "edge01",
		AuthMode: config.AuthModePassword,
		Credential: &ResolvedCredential{
			PasswordResolver: func(context.Context) (*secret.Secret, error) {
				resolverCalls.Add(1)
				return secret.NewFromString("secret"), nil
			},
		},
	}, nil, config.DefaultConfig(), Options{})
	if future == nil || !muxHot {
		t.Fatalf("future=%v muxHot=%v, want lazy future and hot mux", future, muxHot)
	}
	defer future.Close()
	if got := resolverCalls.Load(); got != 0 {
		t.Fatalf("resolver calls before prompt = %d, want 0", got)
	}
	if _, err := future.Resolve(context.Background()); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := resolverCalls.Load(); got != 1 {
		t.Fatalf("resolver calls after lazy resolve = %d, want 1", got)
	}
}

func TestPreparePasswordPrefetchWithoutControlPathStartsPrefetch(t *testing.T) {
	oldCheckMuxSession := checkMuxSessionFunc
	defer func() { checkMuxSessionFunc = oldCheckMuxSession }()
	checkMuxSessionFunc = func(ctx context.Context, req connector.MuxCheckRequest) (bool, bool) {
		return connector.CheckMuxSession(ctx, req, func(context.Context, []string) error {
			t.Fatal("mux check should not execute without ControlPath")
			return nil
		})
	}

	resolverStarted := make(chan struct{})
	future, muxHot := preparePasswordPrefetch(context.Background(), &ResolvedHost{
		Hostname: "edge01",
		AuthMode: config.AuthModePassword,
		Credential: &ResolvedCredential{
			PasswordResolver: func(ctx context.Context) (*secret.Secret, error) {
				close(resolverStarted)
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
	}, nil, config.DefaultConfig(), Options{})
	if future == nil || muxHot {
		t.Fatalf("future=%v muxHot=%v, want prefetch future and cold mux", future, muxHot)
	}
	defer future.Close()

	select {
	case <-resolverStarted:
	case <-time.After(time.Second):
		t.Fatal("password prefetch did not start")
	}
}

func TestAppendResolvedHostCompatFixesCreatesProviderHostOverlay(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"netbox-prod": {
			Type:   config.ProviderNetBox,
			Groups: map[string]config.GroupConfig{"cbb": {}},
		},
	}
	cfg.Inventory.Provider = cfg.Inventory.Providers
	resolved := &ResolvedHost{
		Canonical: "701-sw37r103c608.expedient.com",
		Hostname:  "701-sw37r103c608.expedient.com",
		Provider:  "netbox-prod",
		Group:     "cbb",
	}

	if err := appendResolvedHostCompatFixes(cfg, resolved, []compat.FloorSelection{
		{Category: compat.CategoryKex, Directive: "KexAlgorithms", Floor: "diffie-hellman-group14-sha1"},
		{Category: compat.CategoryKex, Directive: "KexAlgorithms", Floor: "diffie-hellman-group14-sha1"},
	}); err != nil {
		t.Fatalf("appendResolvedHostCompatFixes: %v", err)
	}

	host := cfg.Inventory.Providers["netbox-prod"].Hosts["701-sw37r103c608.expedient.com"]
	if host.Group != "cbb" {
		t.Fatalf("host group = %q, want cbb", host.Group)
	}
	if got := host.SSH.Compatibility.Kex; got != "diffie-hellman-group14-sha1" {
		t.Fatalf("host compatibility.kex = %q, want group14", got)
	}
}

func TestCompatibilityFixesApplyWithHardenedAlgorithmDefaults(t *testing.T) {
	sshConfig := config.SSHHostConfig{
		Options: config.SSHOptions{
			"KexAlgorithms": config.NewSSHOptionItems(
				"sntrup761x25519-sha512@openssh.com",
				"curve25519-sha256",
				"curve25519-sha256@libssh.org",
			),
		},
	}
	output := "Unable to negotiate with 192.0.2.1 port 22: no matching key exchange method found. Their offer: diffie-hellman-group14-sha1,diffie-hellman-group1-sha1"

	fixes := compatibilityFixesToApply(sshConfig, output)

	if len(fixes) != 1 {
		t.Fatalf("fixes = %#v, want one kex fix", fixes)
	}
	if fixes[0].Category != compat.CategoryKex || fixes[0].Floor != "diffie-hellman-group14-sha1" {
		t.Fatalf("fix = %#v, want kex group14 floor", fixes[0])
	}
}
