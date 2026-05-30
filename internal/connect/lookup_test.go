package connect

import (
	"errors"
	"testing"
)

func TestResolveHostnameMissReturnsHostNotFound(t *testing.T) {
	_, err := ResolveHostname("codex-no-such-host-refresh-regression")
	var notFound *HostNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("ResolveHostname error = %v, want HostNotFoundError", err)
	}
	if notFound.Hostname != "codex-no-such-host-refresh-regression" {
		t.Fatalf("not found hostname = %q", notFound.Hostname)
	}
}
