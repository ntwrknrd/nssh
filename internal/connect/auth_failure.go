package connect

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

type credentialFailure struct {
	role  string
	cause error
}

func (e *credentialFailure) Error() string {
	return fmt.Sprintf("resolve %s credential: %s", e.role, e.cause)
}
func (e *credentialFailure) Unwrap() error { return e.cause }
func authFailure(role string, cause error) error {
	failure := &credentialFailure{role: role, cause: cause}
	return &exit.ExitError{Code: exit.ExitAuthFailed, Message: failure.Error(), Cause: failure}
}

func boundedPasswordResolver(resolve func(context.Context) (*secret.Secret, error), timeout time.Duration) func(context.Context) (*secret.Secret, error) {
	if resolve == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = config.DefaultAuthLookupTimeout
	}
	return func(ctx context.Context) (*secret.Secret, error) {
		lookupCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		password, err := resolve(lookupCtx)
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("credential lookup timed out after %s: %w", timeout, err)
		}
		return password, err
	}
}

func manualFallbackAllowed(fallback string, terminal bool, sshArgs []string, ssh config.SSHHostConfig) bool {
	if !terminal || (fallback != "" && fallback != config.AuthFallbackManual) {
		return false
	}
	args := connector.ComposeSSHOptions(connector.SSHOptionPlan{Runtime: sshArgs, Resolved: ssh})
	return !strings.EqualFold(connector.EffectiveSSHOption(args, "BatchMode"), "yes")
}

// disableFailedResolver changes only the failed role; the other credential must
// never be offered in response to its password prompt.
func disableFailedResolver(err error, resolved *ResolvedHost, resolvers *passwordResolvers, terminal bool, sshArgs []string) bool {
	var failure *credentialFailure
	if !errors.As(err, &failure) {
		return false
	}
	switch failure.role {
	case "target":
		if resolvers.target == nil || !manualFallbackAllowed(resolved.AuthFallback, terminal, sshArgs, resolved.SSH) {
			return false
		}
		resolvers.target = nil
	case "proxy":
		if resolved.Proxy == nil || resolvers.proxy == nil || !manualFallbackAllowed(resolved.Proxy.AuthFallback, terminal, nil, resolved.Proxy.ssh) {
			return false
		}
		// Target BatchMode also denotes an unattended invocation.
		if !manualFallbackAllowed("", terminal, sshArgs, resolved.SSH) {
			return false
		}
		resolvers.proxy = nil
	default:
		return false
	}
	return true
}

func connectionAuthEnv(handle *connectionAskpassHandle) []string {
	// Override inherited askpass channels when falling back to native SSH.
	env := []string{"SSH_ASKPASS_REQUIRE=never", "NSSH_PROXY_ASKPASS_REQUIRE=never"}
	if handle != nil {
		env = append(env, handle.env...)
	}
	return env
}
