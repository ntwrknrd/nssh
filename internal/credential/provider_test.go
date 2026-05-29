package credential

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestProviderStatusUsesSingleActiveBackend(t *testing.T) {
	provider, err := NewProvider(&config.Config{
		Credential: config.CredentialConfig{Type: config.CredentialProviderAge},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	status := provider.Status()
	if status.Type != config.CredentialProviderAge {
		t.Fatalf("type = %q", status.Type)
	}
	if !status.Available {
		t.Fatalf("age provider should be available")
	}
}

func TestUnsupportedProviderRejected(t *testing.T) {
	_, err := NewProvider(&config.Config{
		Credential: config.CredentialConfig{Type: "per-host"},
	})
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
}
