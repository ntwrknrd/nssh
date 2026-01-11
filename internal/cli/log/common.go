// Package log provides CLI commands for managing session recordings.
package log

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/ssh/recording"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// sessionUpdatedTimestamp returns the mtime of a cast file.
func sessionUpdatedTimestamp(record recording.SessionRecord) time.Time {
	info, err := os.Stat(record.CastPath)
	if err != nil {
		return record.FinishedAt
	}
	return info.ModTime()
}

// LoadSessions returns all session records sorted by modification time (newest first).
func LoadSessions(settings recording.RecordingSettings) []recording.SessionRecord {
	return LoadSessionsLimit(settings, 0)
}

// LoadSessionsLimit returns session records, limiting to the N most recent.
// When limit > 0, uses lazy loading to only parse metadata for top N files.
// Records are pre-sorted by mtime (newest first).
func LoadSessionsLimit(settings recording.RecordingSettings, limit int) []recording.SessionRecord {
	return recording.IterSessionRecordsLimit(settings, limit)
}

// sessionDurationSeconds calculates the duration of a session in seconds.
func sessionDurationSeconds(record recording.SessionRecord) int {
	// Try to read from index file first
	indexPath := strings.TrimSuffix(record.CastPath, ".cast") + ".index.json"
	if data, err := os.ReadFile(indexPath); err == nil {
		var payload recording.IndexPayload
		if json.Unmarshal(data, &payload) == nil {
			var total int
			for i := range payload.Sessions {
				session := &payload.Sessions[i]
				duration := session.FinishedAt.Sub(session.StartedAt)
				if duration > 0 {
					total += int(duration.Seconds())
				}
			}
			if total > 0 {
				return total
			}
		}
	}

	// Fallback to record timestamps
	duration := record.FinishedAt.Sub(record.StartedAt)
	if duration > 0 {
		return int(duration.Seconds())
	}
	return 0
}

// formatDuration formats seconds as MM:SS or HH:MM:SS.
func formatDuration(seconds int) string {
	minutes := seconds / 60
	secs := seconds % 60
	hours := minutes / 60
	minutes %= 60

	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%02d:%02d", minutes, secs)
}

// homeReplace replaces home directory with ~ for display.
func homeReplace(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// PrintSessions displays sessions in a table format with optional filter label.
func PrintSessions(records []recording.SessionRecord, filter string) {
	if len(records) == 0 {
		ui.Info("No sessions found.")
		return
	}

	headers := []ui.TableHeader{
		{Title: "Last Updated", Color: "cyan"},
		{Title: "Host", Color: "dim"},
		{Title: "Session", Color: "dim"},
		{Title: "Duration", Color: "dim"},
		{Title: "Cast", Color: ""},
	}

	var rows [][]string
	localTZ := time.Now().Location()

	for _, record := range records {
		mtime := sessionUpdatedTimestamp(record)
		mtimeLocal := mtime.In(localTZ)
		mtimeStr := mtimeLocal.Format("2006-01-02T15:04:05")

		seconds := sessionDurationSeconds(record)
		durationStr := formatDuration(seconds)

		sessionLabel := record.SessionLabel
		if sessionLabel == "" {
			sessionLabel = "-"
		}

		castDisplay := homeReplace(record.CastPath)

		rows = append(rows, []string{
			mtimeStr,
			record.Host,
			sessionLabel,
			durationStr,
			castDisplay,
		})
	}

	// Build table first to get margin
	tbl := ui.BuildTable(headers, rows)
	margin := tbl.LeftMargin()

	// Print filter before table if provided
	if filter != "" {
		ui.InfoWithMargin(margin, "Filter: %s", filter)
	}

	tbl.Render()
	ui.InfoWithMargin(margin, "Total: %d sessions", len(records))
}

// FilterSessionsByPattern filters sessions by regex pattern.
func FilterSessionsByPattern(sessions []recording.SessionRecord, pattern string) ([]recording.SessionRecord, error) {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	var filtered []recording.SessionRecord
	for _, session := range sessions {
		castDisplay := homeReplace(session.CastPath)
		display := fmt.Sprintf("%s %s %s", session.Host, session.StartedAt.Format("2006-01-02"), castDisplay)
		if re.MatchString(display) {
			filtered = append(filtered, session)
		}
	}

	return filtered, nil
}

// FilterSessionsByHost filters sessions by hostname and optional date.
func FilterSessionsByHost(sessions []recording.SessionRecord, host, dateFilter string) []recording.SessionRecord {
	hostLower := strings.ToLower(host)
	var result []recording.SessionRecord

	for _, session := range sessions {
		if !strings.Contains(strings.ToLower(session.Host), hostLower) {
			continue
		}

		if dateFilter != "*" && dateFilter != "" {
			sessionDate := session.StartedAt.Format("2006-01-02")
			if sessionDate != dateFilter {
				continue
			}
		}

		result = append(result, session)
	}

	return result
}

// buildSessionOption creates a display string for fzf selection.
func buildSessionOption(idx int, record recording.SessionRecord) string {
	seconds := sessionDurationSeconds(record)
	durationStr := formatDuration(seconds)
	startedLocal := record.StartedAt.Local()
	startedStr := startedLocal.Format("2006-01-02 15:04:05")

	label := record.SessionLabel
	if label == "" {
		label = "-"
	}

	castDisplay := homeReplace(record.CastPath)

	return fmt.Sprintf("[%03d] %s | %s | %s | %s | %s",
		idx+1, startedStr, record.Host, label, durationStr, castDisplay)
}

// SelectSession prompts user to select a session interactively.
// Uses fzf if available, falls back to huh's filtered select.
func SelectSession(sessions []recording.SessionRecord, prompt string) (*recording.SessionRecord, error) {
	if len(sessions) == 0 {
		return nil, fmt.Errorf("no sessions available")
	}

	// Build option strings for selection
	options := make([]string, len(sessions))
	for i, session := range sessions {
		options[i] = buildSessionOption(i, session)
	}

	selected, err := ui.FuzzySelectString(prompt, options)
	if err != nil {
		return nil, err
	}
	if selected == "" {
		return nil, fmt.Errorf("selection canceled")
	}

	// Find the selected session by matching the option string
	for i, opt := range options {
		if opt == selected {
			return &sessions[i], nil
		}
	}

	return nil, fmt.Errorf("selected session not found")
}

// SelectSessionsMulti prompts user to select multiple sessions interactively.
// Uses fzf if available, falls back to huh's multi-select.
func SelectSessionsMulti(sessions []recording.SessionRecord, prompt string) ([]recording.SessionRecord, error) {
	if len(sessions) == 0 {
		return nil, fmt.Errorf("no sessions available")
	}

	// Build options for selection
	options := make([]ui.FuzzySelectOption, len(sessions))
	for i, session := range sessions {
		options[i] = ui.FuzzySelectOption{
			Label: buildSessionOption(i, session),
			Value: i,
		}
	}

	indices, err := ui.FuzzySelectMulti(prompt, options)
	if err != nil {
		return nil, err
	}
	if len(indices) == 0 {
		return nil, fmt.Errorf("selection canceled")
	}

	selected := make([]recording.SessionRecord, len(indices))
	for i, idx := range indices {
		selected[i] = sessions[idx]
	}

	return selected, nil
}

// RequireBinary ensures an external binary is present in PATH.
func RequireBinary(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("'%s' not found in PATH", name)
	}
	return path, nil
}

// RunCommand executes a command, respecting dry-run mode.
func RunCommand(cmd []string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[dry-run] %s\n", strings.Join(cmd, " "))
		return nil
	}

	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	return c.Run()
}

// progressPattern matches agg's progress output: "60 / 60 [===] 100.00 % 27.06/s"
var progressPattern = regexp.MustCompile(`(\d+)\s*/\s*(\d+)\s*\[`)

// RunCommandWithProgress executes a command and displays a styled progress bar.
// It captures stderr to parse progress info and renders the UI-styled bar.
func RunCommandWithProgress(cmd []string, label string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[dry-run] %s\n", strings.Join(cmd, " "))
		return nil
	}

	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdin = os.Stdin
	c.Stderr = os.Stderr

	// Create a pipe for stdout to capture progress (agg outputs progress to stdout)
	stdoutPipe, err := c.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := c.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Read stdout and parse progress
	buf := make([]byte, 1024)
	var lastCurrent, lastTotal int
	pb := ui.NewProgressBar(label, 0)

	for {
		n, err := stdoutPipe.Read(buf)
		if n > 0 {
			line := string(buf[:n])
			if matches := progressPattern.FindStringSubmatch(line); len(matches) >= 3 {
				current := 0
				total := 0
				_, _ = fmt.Sscanf(matches[1], "%d", &current)
				_, _ = fmt.Sscanf(matches[2], "%d", &total)

				if total > 0 && (current != lastCurrent || total != lastTotal) {
					lastCurrent = current
					lastTotal = total
					pb = ui.NewProgressBar(label, total)
					pb.Update(current)
					pb.Print()
				}
			}
		}
		if err != nil {
			break
		}
	}

	// Clear the progress bar line and print final state
	if lastTotal > 0 {
		pb.Clear()
	}

	return c.Wait()
}

// DeleteRecording deletes a recording and its sidecar index file.
func DeleteRecording(castPath, baseDir string, dryRun bool) error {
	indexPath := strings.TrimSuffix(castPath, ".cast") + ".index.json"
	castDisplay := homeReplace(castPath)

	if dryRun {
		ui.Warning("Would delete: %s", castDisplay)
		if _, err := os.Stat(indexPath); err == nil {
			indexDisplay := homeReplace(indexPath)
			ui.Warning("Would delete: %s", indexDisplay)
		}
		return nil
	}

	// Delete cast file
	if err := os.Remove(castPath); err != nil {
		return fmt.Errorf("failed to delete %s: %w", castDisplay, err)
	}
	ui.Success("Deleted: %s", castDisplay)

	// Delete index if exists
	if _, err := os.Stat(indexPath); err == nil {
		_ = os.Remove(indexPath)
	}

	// Cleanup empty directories
	cleanupEmptyDirs(castPath, baseDir)

	return nil
}

// cleanupEmptyDirs removes empty parent directories up to baseDir.
func cleanupEmptyDirs(castPath, baseDir string) {
	dateDir := filepath.Dir(castPath)
	hostDir := filepath.Dir(dateDir)

	// Remove empty date directory
	if entries, err := os.ReadDir(dateDir); err == nil && len(entries) == 0 {
		_ = os.Remove(dateDir)
	}

	// Remove empty host directory
	if hostDir != baseDir {
		if entries, err := os.ReadDir(hostDir); err == nil && len(entries) == 0 {
			_ = os.Remove(hostDir)
		}
	}
}

// DefaultExportDestination creates a friendly export filename.
func DefaultExportDestination(castPath, extension string) string {
	// Path format: <recordings>/<host>/<date>/<session>.cast
	parts := strings.Split(castPath, string(filepath.Separator))

	var hostname, date, session string
	if len(parts) >= 3 {
		session = strings.TrimSuffix(parts[len(parts)-1], ".cast")
		date = parts[len(parts)-2]
		hostname = parts[len(parts)-3]
	} else {
		session = strings.TrimSuffix(filepath.Base(castPath), ".cast")
		hostname = "unknown"
		date = time.Now().Format("2006-01-02")
	}

	baseName := fmt.Sprintf("%s_%s_%s", hostname, date, session)
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, baseName+"."+extension)
}

// MatchesPattern checks if any field matches the pattern.
func MatchesPattern(pattern *regexp.Regexp, fields ...string) bool {
	for _, field := range fields {
		if field != "" && pattern.MatchString(field) {
			return true
		}
	}
	return false
}
