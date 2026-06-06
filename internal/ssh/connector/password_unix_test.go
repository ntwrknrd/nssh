//go:build unix

package connector

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/secret"
)

func TestResolvePasswordUsesLazyResolverOnce(t *testing.T) {
	c := NewConnector("edge01", "netops", nil, nil)
	calls := 0
	c.SetPasswordResolver(func(ctx context.Context) (*secret.Secret, error) {
		calls++
		return secret.NewFromString("secret"), nil
	})

	first, err := c.resolvePassword(context.Background())
	if err != nil {
		t.Fatalf("first resolvePassword: %v", err)
	}
	second, err := c.resolvePassword(context.Background())
	if err != nil {
		t.Fatalf("second resolvePassword: %v", err)
	}
	if first == nil || second == nil {
		t.Fatal("resolved password is nil")
	}
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls)
	}
}

func TestResolvePasswordReturnsExistingPasswordWithoutResolver(t *testing.T) {
	c := NewConnector("edge01", "netops", secret.NewFromString("secret"), nil)

	got, err := c.resolvePassword(context.Background())
	if err != nil {
		t.Fatalf("resolvePassword: %v", err)
	}
	if got == nil {
		t.Fatal("password is nil")
	}
}

func TestResolvePasswordPropagatesResolverError(t *testing.T) {
	c := NewConnector("edge01", "netops", nil, nil)
	c.SetPasswordResolver(func(ctx context.Context) (*secret.Secret, error) {
		return nil, errors.New("provider unavailable")
	})

	got, err := c.resolvePassword(context.Background())
	if err == nil || err.Error() != "provider unavailable" {
		t.Fatalf("error = %v, want provider unavailable", err)
	}
	if got != nil {
		t.Fatalf("password = %+v, want nil", got)
	}
}

func TestResolvePasswordEmitsLazyCredentialLookupTiming(t *testing.T) {
	t.Setenv("NSSH_DEBUG", "1")
	c := NewConnector("edge01", "netops", nil, nil)
	c.SetPasswordResolver(func(ctx context.Context) (*secret.Secret, error) {
		return secret.NewFromString("secret"), nil
	})

	output := captureStderr(t, func() {
		if _, err := c.resolvePassword(context.Background()); err != nil {
			t.Fatalf("resolvePassword: %v", err)
		}
	})

	if !strings.Contains(output, "NSSH_TIMING:"+TimingCredentialLookupLazy+":") {
		t.Fatalf("timing output = %q, want %s", output, TimingCredentialLookupLazy)
	}
}

func TestStartPasswordPrefetchResolvesOnceAndResolvePasswordReusesResult(t *testing.T) {
	c := NewConnector("edge01", "netops", nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	c.SetPasswordResolver(func(ctx context.Context) (*secret.Secret, error) {
		calls++
		close(started)
		<-release
		return secret.NewFromString("secret"), nil
	})

	c.StartPasswordPrefetch(context.Background())
	<-started
	close(release)

	first, err := c.resolvePassword(context.Background())
	if err != nil {
		t.Fatalf("first resolvePassword: %v", err)
	}
	second, err := c.resolvePassword(context.Background())
	if err != nil {
		t.Fatalf("second resolvePassword: %v", err)
	}
	if first == nil || second == nil {
		t.Fatal("resolved password is nil")
	}
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls)
	}
}

func TestStartPasswordPrefetchEmitsPrefetchTimingWithoutLazyTiming(t *testing.T) {
	t.Setenv("NSSH_DEBUG", "1")
	c := NewConnector("edge01", "netops", nil, nil)
	c.SetPasswordResolver(func(ctx context.Context) (*secret.Secret, error) {
		return secret.NewFromString("secret"), nil
	})

	output := captureStderr(t, func() {
		c.StartPasswordPrefetch(context.Background())
		if _, err := c.resolvePassword(context.Background()); err != nil {
			t.Fatalf("resolvePassword: %v", err)
		}
	})

	if !strings.Contains(output, "NSSH_TIMING:"+TimingCredentialLookupPrefetch+":") {
		t.Fatalf("timing output = %q, want %s", output, TimingCredentialLookupPrefetch)
	}
	if strings.Contains(output, "NSSH_TIMING:"+TimingCredentialLookupLazy+":") {
		t.Fatalf("timing output = %q, did not want %s", output, TimingCredentialLookupLazy)
	}
}

func TestStartPasswordPrefetchPropagatesErrorWithoutSecondResolverCall(t *testing.T) {
	c := NewConnector("edge01", "netops", nil, nil)
	calls := 0
	c.SetPasswordResolver(func(ctx context.Context) (*secret.Secret, error) {
		calls++
		return nil, errors.New("provider unavailable")
	})

	c.StartPasswordPrefetch(context.Background())
	got, err := c.resolvePassword(context.Background())
	if err == nil || err.Error() != "provider unavailable" {
		t.Fatalf("error = %v, want provider unavailable", err)
	}
	if got != nil {
		t.Fatalf("password = %+v, want nil", got)
	}
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls)
	}
}

func TestInjectPasswordEmitsPasswordWriteTiming(t *testing.T) {
	t.Setenv("NSSH_DEBUG", "1")
	readFile, writeFile, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = readFile.Close() }()
	defer func() { _ = writeFile.Close() }()

	c := NewConnector("edge01", "netops", secret.NewFromString("secret"), nil)
	c.ptyFile = writeFile

	output := captureStderr(t, func() {
		if err := c.injectPassword(context.Background()); err != nil {
			t.Fatalf("injectPassword: %v", err)
		}
	})

	if !strings.Contains(output, "NSSH_TIMING:"+TimingPasswordWrite+":") {
		t.Fatalf("timing output = %q, want %s", output, TimingPasswordWrite)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	oldStderr := os.Stderr
	readFile, writeFile, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = writeFile
	defer func() {
		os.Stderr = oldStderr
		_ = readFile.Close()
	}()

	fn()

	if err := writeFile.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	data, err := io.ReadAll(readFile)
	if err != nil {
		t.Fatalf("read stderr pipe: %v", err)
	}
	return string(data)
}
