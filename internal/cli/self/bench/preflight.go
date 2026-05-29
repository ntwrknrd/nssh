package bench

import (
	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/vault"
)

func runVaultUnlockPreflight() {
	cfg, err := config.LoadDefault()
	if err != nil || !needsVaultUnlockPreflight(cfg) {
		return
	}
	if mgr, err := clisession.NewManager(vault.Auto()); err == nil {
		_ = clisession.TryUnlockIfTTY(mgr)
	}
}

func needsVaultUnlockPreflight(cfg *config.Config) bool {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return cfg.Credential.Type == config.CredentialProviderAge
}
