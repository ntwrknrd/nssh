package unlock

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestRunWithConfigSkipsExternalCredentialProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Type = config.CredentialProvider1Password
	cfg.Credential.Config.Vault = "Network"

	if err := runWithConfig(false, cfg); err != nil {
		t.Fatalf("runWithConfig: %v", err)
	}
}
