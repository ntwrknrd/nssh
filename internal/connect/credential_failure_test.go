package connect

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/askpass"
	"github.com/ntwrknrd/nssh/internal/ssh/captured"
)

func requestAskpassFailure(t *testing.T, env []string, proxy bool) {
	t.Helper()
	pathKey, nonceKey := askpass.SocketEnv, askpass.NonceEnv
	if proxy {
		pathKey, nonceKey = askpass.ProxySocketEnv, askpass.ProxyNonceEnv
	}
	var path, nonce string
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if key == pathKey {
			path = value
		}
		if key == nonceKey {
			nonce = value
		}
	}
	if path == "" || nonce == "" {
		t.Fatal("missing request-scoped askpass channel")
	}
	if _, err := askpass.RequestPassword(context.Background(), path, nonce); err == nil {
		t.Fatal("failed credential lookup returned a password")
	}
}

func TestCapturedCredentialFailureCancelsSSHAndPreservesAuthError(t *testing.T) {
	oldHelper, oldRun, oldProbe := askpassHelperPathFunc, runCapturedCommandFunc, hostKeyProbeFunc
	t.Cleanup(func() {
		askpassHelperPathFunc, runCapturedCommandFunc, hostKeyProbeFunc = oldHelper, oldRun, oldProbe
	})
	askpassHelperPathFunc = func() (string, error) { return "/tmp/nssh-askpass", nil }
	hostKeyProbeFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) hostKeyProbeStatus {
		return hostKeyProbeClean
	}
	for _, role := range []string{"target", "proxy"} {
		t.Run(role, func(t *testing.T) {
			failure := errors.New("decryption key unavailable")
			credential := &ResolvedCredential{PasswordResolver: func(context.Context) (*secret.Secret, error) {
				return nil, failure
			}}
			resolved := &ResolvedHost{Hostname: "edge01", Port: 22}
			if role == "proxy" {
				resolved.Proxy = &ResolvedProxy{AuthMode: config.AuthModePassword, Credential: credential}
			} else {
				resolved.AuthMode, resolved.Credential = config.AuthModePassword, credential
			}
			runCapturedCommandFunc = func(ctx context.Context, req captured.Request) (captured.Result, error) {
				requestAskpassFailure(t, req.Env, role == "proxy")
				<-ctx.Done()
				return captured.Result{ExitCode: -1, Err: ctx.Err()}, ctx.Err()
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result, err := captureResolvedRemoteCommand(ctx, resolved, nil, []string{"show"}, config.DefaultConfig(), Options{})
			var authErr *exit.ExitError
			if !errors.As(err, &authErr) || authErr.Code != exit.ExitAuthFailed || !errors.Is(err, failure) {
				t.Fatalf("error = %v, want original authentication failure", err)
			}
			if !strings.Contains(err.Error(), "resolve "+role+" credential") {
				t.Fatalf("missing credential role in error: %v", err)
			}
			if result.ExitCode != exit.ExitAuthFailed || !errors.Is(result.Err, failure) {
				t.Fatalf("captured result = %+v, want original auth failure and exit 3", result)
			}
			if ctx.Err() != nil {
				t.Fatal("SSH waited for the outer deadline")
			}
		})
	}
}

func TestExternalAskpassCredentialFailureCancelsConsumer(t *testing.T) {
	oldHelper := askpassHelperPathFunc
	t.Cleanup(func() { askpassHelperPathFunc = oldHelper })
	askpassHelperPathFunc = func() (string, error) { return "/tmp/nssh-askpass", nil }
	failure := errors.New("decryption key unavailable")
	env, err := StartResolvedAskpassEnvironment(context.Background(), &ResolvedHost{
		AuthMode: config.AuthModePassword,
		Credential: &ResolvedCredential{PasswordResolver: func(context.Context) (*secret.Secret, error) {
			return nil, failure
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer env.Cleanup()
	requestAskpassFailure(t, env.Env, false)
	select {
	case <-env.Context(context.Background()).Done():
	case <-time.After(time.Second):
		t.Fatal("external consumer was not canceled")
	}
	if !errors.Is(env.OperationError(context.Canceled), failure) {
		t.Fatal("external consumer lost credential error")
	}
}

func TestCapturedRemoteExit255IsNotReclassifiedAsAuthenticationFailure(t *testing.T) {
	oldRun := runCapturedCommandFunc
	t.Cleanup(func() { runCapturedCommandFunc = oldRun })
	failure := &exit.ExitError{Code: 255, Message: "remote command exited with code 255"}
	runCapturedCommandFunc = func(context.Context, captured.Request) (captured.Result, error) {
		return captured.Result{ExitCode: 255, Stderr: []byte("Permission denied in application\n"), Err: failure}, failure
	}
	result, err := captureResolvedRemoteCommand(context.Background(), &ResolvedHost{Hostname: "edge", Port: 22}, nil, []string{"application"}, config.DefaultConfig(), Options{})
	if !errors.Is(err, failure) || result.ExitCode != 255 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
