package bench

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestNeedsVaultUnlockPreflight(t *testing.T) {
	ageCfg := config.DefaultConfig()
	ageCfg.Credential.Type = config.CredentialProviderAge
	if !needsVaultUnlockPreflight(ageCfg) {
		t.Fatal("age credential provider should unlock vault before benchmark")
	}

	onePasswordCfg := config.DefaultConfig()
	onePasswordCfg.Credential.Type = config.CredentialProvider1Password
	if needsVaultUnlockPreflight(onePasswordCfg) {
		t.Fatal("1Password credential provider should not unlock local vault before benchmark")
	}
}
