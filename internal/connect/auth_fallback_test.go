package connect

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/askpass"
)

func TestManualFallbackRequiresTerminalAndHonorsHostPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		terminal     bool
		args         []string
		want         bool
	}{
		{name: "interactive default", terminal: true, want: true},
		{name: "unattended", policy: "manual"},
		{name: "host disabled", terminal: true, policy: "fail"},
		{name: "BatchMode", terminal: true, args: []string{"-o", "BatchMode=yes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolve := func(context.Context) (*secret.Secret, error) { return nil, nil }
			resolvers := passwordResolvers{target: resolve, proxy: resolve}
			host := &ResolvedHost{AuthFallback: tc.policy, Proxy: &ResolvedProxy{}}
			got := disableFailedResolver(authFailure("target", errors.New("key missing")), host, &resolvers, tc.terminal, tc.args)
			if got != tc.want {
				t.Fatalf("fallback=%v want %v", got, tc.want)
			}
			if resolvers.proxy == nil {
				t.Fatal("target failure changed proxy credential")
			}
			if got && disableFailedResolver(authFailure("target", errors.New("retry")), host, &resolvers, tc.terminal, tc.args) {
				t.Fatal("same role retried twice")
			}
		})
	}
}

func TestProxyFallbackUsesProxyPolicy(t *testing.T) {
	resolve := func(context.Context) (*secret.Secret, error) { return nil, nil }
	resolvers := passwordResolvers{target: resolve, proxy: resolve}
	host := &ResolvedHost{AuthFallback: "fail", Proxy: &ResolvedProxy{AuthFallback: "manual"}}
	if !disableFailedResolver(authFailure("proxy", errors.New("key missing")), host, &resolvers, true, nil) {
		t.Fatal("proxy did not use its policy")
	}
	if resolvers.target == nil {
		t.Fatal("proxy failure changed target credential")
	}
	if disableFailedResolver(context.Canceled, host, &resolvers, true, nil) {
		t.Fatal("retried cancellation")
	}
}

func TestLookupDeadlinePreservedAsAuthFailure(t *testing.T) {
	oldHelper := askpassHelperPathFunc
	t.Cleanup(func() { askpassHelperPathFunc = oldHelper })
	askpassHelperPathFunc = func() (string, error) { return "/tmp/askpass", nil }
	resolve := boundedPasswordResolver(func(ctx context.Context) (*secret.Secret, error) { <-ctx.Done(); return nil, ctx.Err() }, 20*time.Millisecond)
	handle, err := startConnectionAskpass(context.Background(), passwordResolvers{target: resolve})
	if err != nil {
		t.Fatal(err)
	}
	defer handle.cleanup()
	requestAskpassFailure(t, handle.env, false)
	failure := handle.operationError(context.Canceled)
	var authErr *exit.ExitError
	if !errors.As(failure, &authErr) || authErr.Code != 3 || !strings.Contains(failure.Error(), "timed out after 20ms") {
		t.Fatalf("error=%v", failure)
	}
}

func TestPasswordRetriesAreScopedToSSHProcess(t *testing.T) {
	oldHelper := askpassHelperPathFunc
	t.Cleanup(func() { askpassHelperPathFunc = oldHelper })
	askpassHelperPathFunc = func() (string, error) { return "/tmp/askpass", nil }
	password := secret.NewFromString("dummy")
	defer password.Destroy()
	handle, err := startConnectionAskpass(context.Background(), passwordResolvers{proxy: func(context.Context) (*secret.Secret, error) { return password, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer handle.cleanup()
	server := handle.servers[0].server
	for _, pid := range []int{100, 200} {
		if _, err := askpass.RequestPasswordForProcess(context.Background(), server.SocketPath(), server.Nonce(), pid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := askpass.RequestPasswordForProcess(context.Background(), server.SocketPath(), server.Nonce(), 200); err == nil {
		t.Fatal("sent automatic password twice to same SSH process")
	}
	if !strings.Contains(handle.operationError(nil).Error(), "additional authentication challenge") {
		t.Fatal("missing rejection diagnostic")
	}
}
