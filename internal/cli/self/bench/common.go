package bench

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// TimingSample represents a single timing measurement.
type TimingSample struct {
	Name     string
	Duration time.Duration
}

// BenchmarkResult holds the results of a benchmark run.
type BenchmarkResult struct {
	Samples      []map[string]time.Duration // Per-run timing data
	WallClocks   []time.Duration            // Total wall clock per run
	StageNames   []string                   // Ordered list of stage names
	TotalRuns    int
	WarmupRuns   int
	MeasuredRuns int
}

// StageStats holds statistics for a single timing stage.
type StageStats struct {
	Name   string
	Mean   time.Duration
	Median time.Duration
	Min    time.Duration
	Max    time.Duration
}

// findBinary returns the path to the nssh binary for benchmarking.
func findBinary() (string, error) {
	// Check PATH first
	if p, err := exec.LookPath("nssh"); err == nil {
		return p, nil
	}

	// Check common locations
	home, _ := os.UserHomeDir()
	locations := []string{
		home + "/.local/bin/nssh",
		home + "/bin/nssh",
		home + "/go/bin/nssh",
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc, nil
		}
	}

	return "", fmt.Errorf("nssh binary not found")
}

// run executes the benchmark command multiple times and collects timing data.
func run(cmdArgs []string, warmups, samples int, simpleOnly bool) (*BenchmarkResult, error) {
	binary, err := findBinary()
	if err != nil {
		return nil, err
	}

	result := &BenchmarkResult{
		WarmupRuns:   warmups,
		MeasuredRuns: samples,
		TotalRuns:    warmups + samples,
	}

	stageSet := make(map[string]bool)

	for i := 0; i < result.TotalRuns; i++ {
		isWarmup := i < warmups

		// Build command
		cmd := exec.Command(binary, cmdArgs...)

		// Set environment
		env := os.Environ()
		if !simpleOnly {
			env = append(env, "NSSH_DEBUG=1")
		}
		// Always disable recording during benchmarks to prevent asciinema
		// from opening /dev/tty directly and bypassing our output capture
		env = append(env, "NSSH_RECORD=0")
		cmd.Env = env

		// Capture stderr for timing data, stdin/stdout to /dev/null
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cmd.Stdin = nil
		cmd.Stdout = nil

		// Run and time
		start := time.Now()
		err := cmd.Run()
		wallClock := time.Since(start)

		if err != nil {
			// Some errors are expected (like connection test commands)
			// Continue unless it's a fundamental issue
			if exitErr, ok := err.(*exec.ExitError); ok {
				// Exit code 255 often means connection issue
				if exitErr.ExitCode() == 255 && i == 0 {
					return nil, fmt.Errorf("connection failed on first attempt: %w", err)
				}
			}
		}

		if isWarmup {
			fmt.Printf("  Warmup %d/%d: %s\n", i+1, warmups, ui.Cyan(formatDuration(wallClock)))
			continue
		}

		// Parse timing data from stderr
		timings := parseTimingOutput(stderr.String())
		for name := range timings {
			stageSet[name] = true
		}

		result.Samples = append(result.Samples, timings)
		result.WallClocks = append(result.WallClocks, wallClock)

		sampleNum := i - warmups + 1
		fmt.Printf("  Sample %d/%d: %s\n", sampleNum, samples, ui.Cyan(formatDuration(wallClock)))
	}

	// Build ordered stage names
	for name := range stageSet {
		result.StageNames = append(result.StageNames, name)
	}
	sort.Strings(result.StageNames)

	return result, nil
}

// parseTimingOutput extracts timing data from stderr output.
// Format: NSSH_TIMING:<event>:<duration_ms>
func parseTimingOutput(output string) map[string]time.Duration {
	timings := make(map[string]time.Duration)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "NSSH_TIMING:") {
			continue
		}

		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}

		name := parts[1]
		msStr := parts[2]

		ms, err := strconv.ParseFloat(msStr, 64)
		if err != nil {
			continue
		}

		timings[name] = time.Duration(ms * float64(time.Millisecond))
	}

	return timings
}

// ComputeAllStats calculates statistics for all stages in a single pass.
func ComputeAllStats(samples []map[string]time.Duration, stageNames []string) map[string]StageStats {
	// Build duration slices for each stage in one pass
	stageDurations := make(map[string][]time.Duration)
	for _, stageName := range stageNames {
		stageDurations[stageName] = make([]time.Duration, 0, len(samples))
	}

	// Single pass through samples
	for _, sample := range samples {
		for _, stageName := range stageNames {
			if d, ok := sample[stageName]; ok {
				stageDurations[stageName] = append(stageDurations[stageName], d)
			}
		}
	}

	// Compute stats for each stage
	results := make(map[string]StageStats)
	for stageName, durations := range stageDurations {
		results[stageName] = computeStatsFromDurations(stageName, durations)
	}
	return results
}

// computeStatsFromDurations calculates stats from a pre-extracted duration slice.
func computeStatsFromDurations(name string, durations []time.Duration) StageStats {
	if len(durations) == 0 {
		return StageStats{Name: name}
	}

	// Sort for min/max/median
	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	// Calculate mean
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	mean := total / time.Duration(len(durations))

	// Calculate median
	median := durations[len(durations)/2]
	if len(durations)%2 == 0 {
		median = (durations[len(durations)/2-1] + durations[len(durations)/2]) / 2
	}

	return StageStats{
		Name:   name,
		Mean:   mean,
		Median: median,
		Min:    durations[0],
		Max:    durations[len(durations)-1],
	}
}

// ComputeWallClockStats calculates statistics for wall clock times.
func ComputeWallClockStats(wallClocks []time.Duration) StageStats {
	if len(wallClocks) == 0 {
		return StageStats{Name: "total"}
	}

	sorted := make([]time.Duration, len(wallClocks))
	copy(sorted, wallClocks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	var total time.Duration
	for _, d := range sorted {
		total += d
	}
	mean := total / time.Duration(len(sorted))

	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}

	return StageStats{
		Name:   "total",
		Mean:   mean,
		Median: median,
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
	}
}

// renderResults displays benchmark results in a formatted table.
func renderResults(result *BenchmarkResult, simpleOnly bool) {
	fmt.Println()

	// Stage breakdown (if detailed mode)
	if !simpleOnly && len(result.StageNames) > 0 {
		// Sort stages in logical order
		sortedStages := sortStageNames(result.StageNames)

		// Compute all stats in a single pass (avoids O(stages × samples) overhead)
		allStats := ComputeAllStats(result.Samples, result.StageNames)
		wallStats := ComputeWallClockStats(result.WallClocks)

		table := ui.NewTable("Stage", "Mean", "Median", "Min", "Max", "Description")

		// Add startup overhead first (wall_clock - connector_total)
		// This represents Go subprocess initialization before connector.Run()
		connectorStats := allStats[connector.TimingTotal]
		if connectorStats.Mean > 0 && wallStats.Mean > connectorStats.Mean {
			table.AddRow(
				"startup",
				formatDuration(wallStats.Mean-connectorStats.Mean),
				formatDuration(wallStats.Median-connectorStats.Median),
				formatDuration(wallStats.Min-connectorStats.Min),
				formatDuration(wallStats.Max-connectorStats.Max),
				ui.Gray("Go subprocess initialization"),
			)
		}

		// Get first_read stats for computing session_io
		firstReadStats := allStats[connector.TimingFirstRead]

		for _, stageName := range sortedStages {
			// Skip total - we show wall_clock as the total instead
			if stageName == connector.TimingTotal {
				continue
			}

			stats := allStats[stageName]
			desc := StageDescriptions[stageName]
			if desc == "" {
				desc = "-"
			}

			// Convert session_end from cumulative to delta (session_io = session_end - first_read)
			if stageName == connector.TimingSessionEnd {
				if firstReadStats.Mean > 0 {
					// Compute delta: time after first_read until session close
					deltaMean := stats.Mean - firstReadStats.Mean
					deltaMedian := stats.Median - firstReadStats.Median
					deltaMin := stats.Min - firstReadStats.Min
					deltaMax := stats.Max - firstReadStats.Max
					table.AddRow(
						"session_io",
						formatDuration(deltaMean),
						formatDuration(deltaMedian),
						formatDuration(deltaMin),
						formatDuration(deltaMax),
						ui.Gray("I/O after first read until session close"),
					)
					continue
				}
			}

			table.AddRow(
				stageName,
				formatDuration(stats.Mean),
				formatDuration(stats.Median),
				formatDuration(stats.Min),
				formatDuration(stats.Max),
				ui.Gray(desc),
			)
		}

		// Add wall_clock as total footer (the actual end-to-end time)
		table.AddFooterRow(
			"total",
			formatDuration(wallStats.Mean),
			formatDuration(wallStats.Median),
			formatDuration(wallStats.Min),
			formatDuration(wallStats.Max),
			"End-to-end time",
		)

		table.Render()
	} else {
		// Simple mode - just show wall clock range
		stats := ComputeWallClockStats(result.WallClocks)
		fmt.Printf("  Wall Clock: %s - %s\n", formatDuration(stats.Min), formatDuration(stats.Max))
	}
}

// formatDuration formats a duration for display in benchmarks.
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fus", float64(d.Nanoseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Nanoseconds())/1e6)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// TimingStageOrder defines the logical order of timing stages for display.
var TimingStageOrder = []string{
	connector.TimingConfigLoad,
	connector.TimingCredentialLookup,
	connector.TimingPTYStart,
	connector.TimingFirstRead,
	connector.TimingPasswordPrompt,
	connector.TimingPasswordSent,
	connector.TimingSessionEnd,
	connector.TimingTotal,
}

// StageDescriptions provides human-readable descriptions for timing stages.
var StageDescriptions = map[string]string{
	connector.TimingConfigLoad:       "Load config.toml",
	connector.TimingCredentialLookup: "Vault credential resolution",
	connector.TimingPTYStart:         "Spawn PTY + SSH process",
	connector.TimingFirstRead:        "Time to first SSH data (banner/prompt)",
	connector.TimingPasswordPrompt:   "Time to password prompt (from session start)",
	connector.TimingPasswordSent:     "Password injection duration",
	connector.TimingSessionEnd:       "Total session duration (from session start)",
	connector.TimingTotal:            "Connector.Run() total time",
}

// stageOrderIndex maps stage names to their display order.
var stageOrderIndex = func() map[string]int {
	m := make(map[string]int)
	for i, s := range TimingStageOrder {
		m[s] = i
	}
	return m
}()

// sortStageNames sorts stage names in logical order (per TimingStageOrder).
// Unknown stages are sorted alphabetically at the end.
func sortStageNames(stages []string) []string {
	sorted := make([]string, len(stages))
	copy(sorted, stages)
	sort.Slice(sorted, func(i, j int) bool {
		iIdx, iKnown := stageOrderIndex[sorted[i]]
		jIdx, jKnown := stageOrderIndex[sorted[j]]
		if iKnown && jKnown {
			return iIdx < jIdx
		}
		if iKnown {
			return true // known stages come first
		}
		if jKnown {
			return false
		}
		return sorted[i] < sorted[j] // alphabetical for unknown
	})
	return sorted
}

// benchmarksDir returns the path to the benchmarks directory.
func benchmarksDir() string {
	return filepath.Join(config.DefaultPaths().DataDir, "benchmarks")
}

// PrintSavedPath prints the saved file path.
func PrintSavedPath(path string) {
	if path == "" {
		return
	}
	fmt.Printf("\n  %s: %s\n", ui.Gray("Saved"), path)
}

// SaveResults saves benchmark results to a timestamped file and updates symlinks.
// Returns the path to the saved file, or empty string if saving failed.
func SaveResults(benchType, host string, result *BenchmarkResult, simpleOnly bool) string {
	dir := benchmarksDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ""
	}

	// Generate timestamped filename
	timestamp := time.Now().Format("2006-01-02-150405")
	filename := fmt.Sprintf("%s-%s-%s.txt", benchType, host, timestamp)
	filepath := filepath.Join(dir, filename)

	// Render results to string
	content := renderResultsToString(benchType, host, result, simpleOnly)

	// Write file
	if err := os.WriteFile(filepath, []byte(content), 0644); err != nil {
		return ""
	}

	// Update symlinks
	updateSymlinks(dir, benchType, filename)

	return filepath
}

// renderResultsToString renders benchmark results to a string (for file output).
func renderResultsToString(benchType, host string, result *BenchmarkResult, simpleOnly bool) string {
	var buf strings.Builder

	fmt.Fprintf(&buf, "%s benchmark: %s\n", strings.ToUpper(benchType), host)
	fmt.Fprintf(&buf, "Date: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&buf, "Samples: %d (warmups: %d)\n\n", result.MeasuredRuns, result.WarmupRuns)

	if !simpleOnly && len(result.StageNames) > 0 {
		sortedStages := sortStageNames(result.StageNames)
		allStats := ComputeAllStats(result.Samples, result.StageNames)
		wallStats := ComputeWallClockStats(result.WallClocks)

		// Header
		fmt.Fprintf(&buf, "%-20s %10s %10s %10s %10s\n", "Stage", "Mean", "Median", "Min", "Max")
		buf.WriteString(strings.Repeat("-", 62) + "\n")

		// Startup overhead
		connectorStats := allStats[connector.TimingTotal]
		if connectorStats.Mean > 0 && wallStats.Mean > connectorStats.Mean {
			fmt.Fprintf(&buf, "%-20s %10s %10s %10s %10s\n",
				"startup",
				formatDuration(wallStats.Mean-connectorStats.Mean),
				formatDuration(wallStats.Median-connectorStats.Median),
				formatDuration(wallStats.Min-connectorStats.Min),
				formatDuration(wallStats.Max-connectorStats.Max),
			)
		}

		firstReadStats := allStats[connector.TimingFirstRead]

		for _, stageName := range sortedStages {
			if stageName == connector.TimingTotal {
				continue
			}

			stats := allStats[stageName]

			if stageName == connector.TimingSessionEnd && firstReadStats.Mean > 0 {
				fmt.Fprintf(&buf, "%-20s %10s %10s %10s %10s\n",
					"session_io",
					formatDuration(stats.Mean-firstReadStats.Mean),
					formatDuration(stats.Median-firstReadStats.Median),
					formatDuration(stats.Min-firstReadStats.Min),
					formatDuration(stats.Max-firstReadStats.Max),
				)
				continue
			}

			fmt.Fprintf(&buf, "%-20s %10s %10s %10s %10s\n",
				stageName,
				formatDuration(stats.Mean),
				formatDuration(stats.Median),
				formatDuration(stats.Min),
				formatDuration(stats.Max),
			)
		}

		buf.WriteString(strings.Repeat("-", 62) + "\n")
		fmt.Fprintf(&buf, "%-20s %10s %10s %10s %10s\n",
			"total",
			formatDuration(wallStats.Mean),
			formatDuration(wallStats.Median),
			formatDuration(wallStats.Min),
			formatDuration(wallStats.Max),
		)
	} else {
		stats := ComputeWallClockStats(result.WallClocks)
		fmt.Fprintf(&buf, "Wall Clock: %s - %s\n", formatDuration(stats.Min), formatDuration(stats.Max))
	}

	return buf.String()
}

// updateSymlinks updates the latest and previous symlinks.
func updateSymlinks(dir, benchType, newFile string) {
	latestLink := filepath.Join(dir, benchType+"-latest.txt")
	previousLink := filepath.Join(dir, benchType+"-previous.txt")

	// Read current latest target (if exists)
	currentTarget, err := os.Readlink(latestLink)
	if err == nil && currentTarget != "" {
		// Move latest -> previous
		_ = os.Remove(previousLink)
		_ = os.Symlink(currentTarget, previousLink)
	}

	// Update latest -> new file
	_ = os.Remove(latestLink)
	_ = os.Symlink(newFile, latestLink)
}
