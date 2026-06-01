package bench

import (
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func TestComputeSessionIODeltasUsesPerSampleDurations(t *testing.T) {
	samples := []map[string]time.Duration{
		{connector.TimingFirstRead: 100 * time.Millisecond, connector.TimingSessionEnd: 140 * time.Millisecond},
		{connector.TimingFirstRead: 90 * time.Millisecond, connector.TimingSessionEnd: 130 * time.Millisecond},
	}

	stats := computeDeltaStats("session_io", samples, connector.TimingSessionEnd, connector.TimingFirstRead)
	if stats.Mean != 40*time.Millisecond || stats.Min != 40*time.Millisecond || stats.Max != 40*time.Millisecond {
		t.Fatalf("stats = %+v, want all 40ms", stats)
	}
}

func TestComputeStartupOverheadUsesPerSampleDurations(t *testing.T) {
	wallClocks := []time.Duration{100 * time.Millisecond, 120 * time.Millisecond}
	samples := []map[string]time.Duration{
		{connector.TimingTotal: 70 * time.Millisecond},
		{connector.TimingTotal: 80 * time.Millisecond},
	}

	stats := computeWallMinusStageStats("pre_connector", wallClocks, samples, connector.TimingTotal)
	if stats.Mean != 35*time.Millisecond || stats.Min != 30*time.Millisecond || stats.Max != 40*time.Millisecond {
		t.Fatalf("stats = %+v", stats)
	}
}
