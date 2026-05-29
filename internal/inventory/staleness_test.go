package inventory

import (
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestProviderIsStale(t *testing.T) {
	now := time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)
	cfg := config.InventoryProviderConfig{RefreshInterval: config.Duration(15 * time.Minute)}

	if !ProviderIsStale(nil, cfg, now) {
		t.Fatal("missing state should be stale")
	}
	if ProviderIsStale(&ProviderState{LastRefresh: now.Add(-5 * time.Minute)}, cfg, now) {
		t.Fatal("recent refresh should not be stale")
	}
	if !ProviderIsStale(&ProviderState{LastRefresh: now.Add(-20 * time.Minute)}, cfg, now) {
		t.Fatal("old refresh should be stale")
	}
	if ProviderIsStale(&ProviderState{LastRefresh: now.Add(-20 * time.Minute)}, config.InventoryProviderConfig{}, now) {
		t.Fatal("provider without refresh interval should not be refreshed automatically")
	}
}
