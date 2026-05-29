package bench

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestNeedsVaultUnlockPreflight(t *testing.T) {
	ageCfg := config.DefaultConfig()
	ageCfg.Credential.Type = "age"
	if needsVaultUnlockPreflight(ageCfg) {
		t.Fatal("benchmarks should not unlock the local vault")
	}

	onePasswordCfg := config.DefaultConfig()
	if needsVaultUnlockPreflight(onePasswordCfg) {
		t.Fatal("provider-backed credentials should authenticate through providers")
	}
}
