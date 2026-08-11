package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestSortStageNamesOrdersLazyCredentialLookupAfterPasswordPrompt(t *testing.T) {
	got := sortStageNames([]string{
		connector.TimingPasswordWrite,
		connector.TimingPasswordSent,
		connector.TimingCredentialLookupLazy,
		connector.TimingPasswordPrompt,
		connector.TimingPTYStart,
	})
	want := []string{
		connector.TimingPTYStart,
		connector.TimingPasswordPrompt,
		connector.TimingCredentialLookupLazy,
		connector.TimingPasswordWrite,
		connector.TimingPasswordSent,
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortStageNames = %v, want %v", got, want)
		}
	}
}

func TestParseTimingOutputKeepsEnhancedDebugStages(t *testing.T) {
	got := parseTimingOutput(strings.Join([]string{
		"NSSH_TIMING:config_load:1.250",
		"NSSH_TIMING:catalog_total:2.500",
		"NSSH_TIMING:ssh_process_total:20.000",
		"debug1: unrelated ssh output",
	}, "\n"))

	if got[connector.TimingConfigLoad] != 1250*time.Microsecond {
		t.Fatalf("config_load = %s, want 1.25ms", got[connector.TimingConfigLoad])
	}
	if got[connector.TimingCatalogTotal] != 2500*time.Microsecond {
		t.Fatalf("catalog_total = %s, want 2.5ms", got[connector.TimingCatalogTotal])
	}
	if got[connector.TimingSSHProcessTotal] != 20*time.Millisecond {
		t.Fatalf("ssh_process_total = %s, want 20ms", got[connector.TimingSSHProcessTotal])
	}
}

func TestParseTimingOutputAggregatesDuplicateStages(t *testing.T) {
	got := parseTimingOutput(strings.Join([]string{
		"NSSH_TIMING:config_load:1.000",
		"NSSH_TIMING:config_load:2.500",
	}, "\n"))

	if got[connector.TimingConfigLoad] != 3500*time.Microsecond {
		t.Fatalf("config_load = %s, want aggregate 3.5ms", got[connector.TimingConfigLoad])
	}
}

func TestEnhancedTimingStagesHaveDescriptionsAndOrder(t *testing.T) {
	stages := []string{
		connector.TimingConfigLoad,
		connector.TimingCatalogTotal,
		connector.TimingProviderStateList,
		connector.TimingProviderStateLoad,
		connector.TimingCatalogLocalHosts,
		connector.TimingCatalogProviderHosts,
		connector.TimingAuthResolve,
		connector.TimingCredentialRegistry,
		connector.TimingCredentialLookup,
		connector.TimingMuxCheck,
		connector.TimingMuxStart,
		connector.TimingCredentialPrefetch,
		connector.TimingAskpassSetup,
		connector.TimingSSHArgsBuild,
		connector.TimingSSHProcessStart,
		connector.TimingSSHProcessWait,
		connector.TimingSSHProcessTotal,
	}

	for _, stage := range stages {
		if StageDescriptions[stage] == "" {
			t.Fatalf("missing description for %s", stage)
		}
	}

	got := sortStageNames([]string{
		connector.TimingSSHProcessTotal,
		connector.TimingCredentialLookup,
		connector.TimingMuxStart,
		connector.TimingConfigLoad,
		connector.TimingCatalogTotal,
		connector.TimingAskpassSetup,
		connector.TimingSSHArgsBuild,
		connector.TimingSSHProcessStart,
	})
	want := []string{
		connector.TimingConfigLoad,
		connector.TimingCatalogTotal,
		connector.TimingCredentialLookup,
		connector.TimingMuxStart,
		connector.TimingAskpassSetup,
		connector.TimingSSHArgsBuild,
		connector.TimingSSHProcessStart,
		connector.TimingSSHProcessTotal,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortStageNames = %v, want %v", got, want)
		}
	}
}

func TestBuildSSHBenchmarkArgsAddsPasswordAuthOptionsBeforeCommand(t *testing.T) {
	got := buildSSHBenchmarkArgs("edge01", true)
	want := []string{
		"edge01",
		"-o", "ControlMaster=no",
		"-o", "ControlPath=none",
		"-o", "ControlPersist=no",
		"-o", "PubkeyAuthentication=no",
		"-o", "PasswordAuthentication=yes",
		"-o", "KbdInteractiveAuthentication=yes",
		"-o", "PreferredAuthentications=password,keyboard-interactive",
		"--", "echo", "nssh-benchmark-test",
	}

	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestSaveResultsWritesRawJSONArtifact(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	result := &BenchmarkResult{
		Samples: []map[string]time.Duration{{
			connector.TimingTotal: 10 * time.Millisecond,
		}},
		WallClocks:   []time.Duration{12 * time.Millisecond},
		StageNames:   []string{connector.TimingTotal},
		TotalRuns:    1,
		MeasuredRuns: 1,
	}

	textPath := SaveResults("ssh", "edge01", result, false)
	jsonPath := textPath[:len(textPath)-len(filepath.Ext(textPath))] + ".json"
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read json artifact: %v", err)
	}

	var artifact rawBenchmarkArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if artifact.Type != "ssh" || artifact.Host != "edge01" {
		t.Fatalf("artifact = %+v", artifact)
	}
	if len(artifact.Samples) != 1 || artifact.Samples[0].Stages[connector.TimingTotal] != 10 {
		t.Fatalf("samples = %+v", artifact.Samples)
	}
	if artifact.Samples[0].WallClockMS != 12 {
		t.Fatalf("wall clock = %v, want 12", artifact.Samples[0].WallClockMS)
	}
}
