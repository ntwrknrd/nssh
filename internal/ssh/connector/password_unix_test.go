//go:build unix

package connector

import (
	"context"
	"errors"
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
