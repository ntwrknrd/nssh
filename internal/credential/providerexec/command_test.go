package providerexec

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCommandRequestDoesNotSignIn(t *testing.T) {
	runner := &fakeOnePasswordRunner{errs: []error{errors.New("not signed in")}}
	e := NewExecutor()
	e.Register1Password("op", OnePasswordProviderConfig{Runner: runner})
	_, err := e.HandleProviderRequest(context.Background(), ProviderRequest{Provider: "op", Action: "get", Ref: "op://vault/item/password", Username: "admin", NonInteractive: true})
	if err == nil || !strings.Contains(err.Error(), "authentication before") {
		t.Fatalf("err=%v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("unexpected signin attempts: %v", runner.calls)
	}
}
