package bench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func TestResolveCredentialBenchmarkTargetRejectsMissingHostBeforeCredentialLookup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	_, err := resolveCredentialBenchmarkTarget("codex-no-such-bench-host")
	if err == nil {
		t.Fatal("resolveCredentialBenchmarkTarget error = nil, want host not found")
	}
	if !strings.Contains(err.Error(), "host not found: codex-no-such-bench-host") {
		t.Fatalf("resolveCredentialBenchmarkTarget error = %q, want host not found", err)
	}
}

func TestRunCredentialBenchmarkForTargetRunsIntegratedPasswordAuth(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	target := &connect.CredentialTarget{
		Host: "edge01",
		Resolver: func(ctx context.Context) (*secret.Secret, error) {
			return secret.NewFromString("secret"), nil
		},
	}

	called := false
	oldRun := runIntegratedCredentialSSHBenchmark
	runIntegratedCredentialSSHBenchmark = func(host string, warmups, samples int, simpleOnly, passwordAuth bool) error {
		called = true
		if host != "edge01" || warmups != 2 || samples != 3 || simpleOnly || !passwordAuth {
			t.Fatalf("args = host=%q warmups=%d samples=%d simpleOnly=%t passwordAuth=%t", host, warmups, samples, simpleOnly, passwordAuth)
		}
		return nil
	}
	defer func() { runIntegratedCredentialSSHBenchmark = oldRun }()

	if err := runCredentialBenchmarkForTarget(target, 2, 3, time.Second); err != nil {
		t.Fatalf("runCredentialBenchmarkForTarget: %v", err)
	}
	if !called {
		t.Fatal("integrated SSH benchmark was not called")
	}
}

func TestRunCredentialTargetBenchmarkRejectsMissingCredential(t *testing.T) {
	target := &connect.CredentialTarget{Host: "edge01"}

	_, err := runCredentialTargetBenchmark(context.Background(), target, 0, 1)
	if err == nil || err.Error() != "host edge01 has no credential resolver" {
		t.Fatalf("error = %v, want no credential resolver", err)
	}
}

func TestRunCredentialTargetBenchmarkRecordsResolverTiming(t *testing.T) {
	target := &connect.CredentialTarget{
		Host:     "edge01",
		Provider: "op-network",
		Resolver: func(ctx context.Context) (*secret.Secret, error) {
			return secret.NewFromString("secret"), nil
		},
	}

	result, err := runCredentialTargetBenchmark(context.Background(), target, 0, 1)
	if err != nil {
		t.Fatalf("runCredentialTargetBenchmark: %v", err)
	}
	if result.MeasuredRuns != 1 || len(result.Samples) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Samples[0][connector.TimingCredentialLookup] <= 0 {
		t.Fatalf("samples = %+v, want credential lookup timing", result.Samples)
	}
}

func TestRunCredentialTargetBenchmarkPropagatesResolverError(t *testing.T) {
	target := &connect.CredentialTarget{
		Host: "edge01",
		Resolver: func(ctx context.Context) (*secret.Secret, error) {
			return nil, errors.New("provider unavailable")
		},
	}

	_, err := runCredentialTargetBenchmark(context.Background(), target, 0, 1)
	if err == nil || err.Error() != "provider unavailable" {
		t.Fatalf("error = %v, want provider unavailable", err)
	}
}

func TestRunCredentialTargetBenchmarkTimesOutProviderLookup(t *testing.T) {
	target := &connect.CredentialTarget{
		Host: "edge01",
		Resolver: func(ctx context.Context) (*secret.Secret, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	_, err := runCredentialTargetBenchmarkWithTimeout(context.Background(), target, 0, 1, time.Nanosecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}
