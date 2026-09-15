package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNonInteractiveProviderActionDoesNotSignIn(t *testing.T) {
	runner := &fakeOnePasswordRunner{errs: []error{errors.New("not signed in")}}
	provider := NewRuntimeProvider()
	defer func() { _ = provider.Close() }()
	provider.Register1Password("op", OnePasswordProviderConfig{Runner: runner})
	_, err := provider.HandleProviderRequest(context.Background(), ProviderRequest{Provider: "op", Action: "get_noninteractive", Ref: "op://v/i/password", Username: "user"})
	if err == nil || !strings.Contains(err.Error(), "authentication before") {
		t.Fatalf("err=%v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("signin attempted: %v", runner.calls)
	}
}
