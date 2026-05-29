package bench

import "github.com/ntwrknrd/nssh/internal/config"

func runVaultUnlockPreflight() {
	// Provider-backed credentials authenticate through their own provider flows.
}

func needsVaultUnlockPreflight(cfg *config.Config) bool {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return false
}
