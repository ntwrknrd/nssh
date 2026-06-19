//go:build unix

package recording

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

// RecordingSettings holds configuration for session recording.
type RecordingSettings struct {
	Enabled           bool
	AppendMode        bool
	IncludePatterns   []HostPattern
	ExcludePatterns   []HostPattern
	Directory         string
	AsciinemaServer   string
	IdleTimeLimit     float64 // seconds, 0 = disabled
	IdleTimeLimitMode string  // "play", "record", or "both"
	TitleFormat       string  // template with {host}, {user}, {date}, {time}
	AutoExportTxt     bool    // export to .txt on session close
}

// HostPattern represents a host matching pattern (glob or regex).
type HostPattern struct {
	Raw     string
	Kind    string // "glob" or "regex"
	Pattern string
	Regex   *regexp.Regexp // Only set for regex patterns
}

// SessionRecord holds metadata about a recorded session.
type SessionRecord struct {
	Host         string
	CastPath     string
	StartedAt    time.Time
	FinishedAt   time.Time
	Argv         []string
	SessionLabel string
}

// SessionUpdatedTimestamp returns the mtime of a cast file.
func SessionUpdatedTimestamp(record SessionRecord) time.Time {
	info, err := os.Stat(record.CastPath)
	if err != nil {
		return record.FinishedAt
	}
	return info.ModTime()
}

// SessionDurationSeconds calculates the duration of a session in seconds.
func SessionDurationSeconds(record SessionRecord) int {
	indexPath := strings.TrimSuffix(record.CastPath, ".cast") + ".index.json"
	if data, err := os.ReadFile(indexPath); err == nil {
		var payload IndexPayload
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

	duration := record.FinishedAt.Sub(record.StartedAt)
	if duration > 0 {
		return int(duration.Seconds())
	}
	return 0
}

// RecordingPlan describes how to record a session.
type RecordingPlan struct {
	Enabled       bool
	Reason        string // Why recording is disabled, if applicable
	CastPath      string
	Append        bool
	Title         string
	AsciinemaPath string
	LockDirectory string
	Sequence      int
	SessionLabel  string
	IdleTimeLimit float64 // seconds, 0 = disabled
	// Warn indicates the user should be warned that recording was skipped.
	// Used for fail-open scenarios such as missing asciinema.
	Warn bool
}

// IndexEntry represents a single session in the index file.
type IndexEntry struct {
	Host       string    `json:"host"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	ExitCode   int       `json:"exit_code"`
	Auth       string    `json:"auth"`
	Argv       []string  `json:"argv"`
	Session    string    `json:"session,omitempty"`
}

// IndexPayload is the structure of the .index.json file.
type IndexPayload struct {
	Host     string       `json:"host"`
	Cast     string       `json:"cast"`
	Sessions []IndexEntry `json:"sessions"`
}

var (
	sessionFilePattern = regexp.MustCompile(`session-(\d+)\.cast$`)
)

// DefaultRecordingSettings returns default recording configuration.
func DefaultRecordingSettings() RecordingSettings {
	paths := config.DefaultPaths()
	return RecordingSettings{
		Enabled:           false, // opt-in by default to avoid first-run failures
		AppendMode:        true,
		Directory:         paths.RecordingsDir,
		IdleTimeLimit:     2.0,
		IdleTimeLimitMode: "play",
		TitleFormat:       "nssh:{host}",
	}
}

// LoadRecordingSettings loads recording settings from config and environment.
func LoadRecordingSettings() RecordingSettings {
	settings := DefaultRecordingSettings()

	// Load from config file
	cfg, err := config.LoadDefault()
	if err == nil {
		session := &cfg.Logging.Session
		if session.Enabled != nil {
			settings.Enabled = *session.Enabled
		}
		if session.AppendMode != nil {
			settings.AppendMode = *session.AppendMode
		}
		if session.Dir != "" {
			settings.Directory = expandPath(session.Dir)
		}
		if session.AsciinemaServer != "" {
			settings.AsciinemaServer = session.AsciinemaServer
		}
		if session.IdleTimeLimit > 0 {
			settings.IdleTimeLimit = session.IdleTimeLimit
		}
		if session.IdleTimeLimitMode != "" {
			settings.IdleTimeLimitMode = session.IdleTimeLimitMode
		}
		if session.TitleFormat != "" {
			settings.TitleFormat = session.TitleFormat
		}
		settings.AutoExportTxt = session.AutoExportTxt
		settings.IncludePatterns = parseHostPatterns(session.IncludeHosts)
		settings.ExcludePatterns = parseHostPatterns(session.ExcludeHosts)
	}

	// Environment overrides
	if envDir := os.Getenv("NSSH_RECORD_DIR"); envDir != "" {
		settings.Directory = expandPath(envDir)
	}

	if envRecord := os.Getenv("NSSH_RECORD"); envRecord != "" {
		switch strings.ToLower(strings.TrimSpace(envRecord)) {
		case "0", "false", "off":
			settings.Enabled = false
		case "1", "true", "on":
			settings.Enabled = true
		}
	}

	if envIdleLimit := os.Getenv("NSSH_RECORD_IDLE_TIME_LIMIT"); envIdleLimit != "" {
		if val, err := strconv.ParseFloat(envIdleLimit, 64); err == nil && val > 0 {
			settings.IdleTimeLimit = val
		}
	}

	if envIdleMode := os.Getenv("NSSH_RECORD_IDLE_TIME_LIMIT_MODE"); envIdleMode != "" {
		mode := strings.ToLower(strings.TrimSpace(envIdleMode))
		if mode == "play" || mode == "record" || mode == "both" {
			settings.IdleTimeLimitMode = mode
		}
	}

	if envTitleFormat := os.Getenv("NSSH_RECORD_TITLE_FORMAT"); envTitleFormat != "" {
		settings.TitleFormat = envTitleFormat
	}

	return settings
}

// ShouldRecord returns true if the given hostname should be recorded.
func ShouldRecord(hostname string, settings RecordingSettings) bool {
	if !settings.Enabled {
		return false
	}

	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return false
	}

	// Check exclusions first
	if len(settings.ExcludePatterns) > 0 && matchesPatterns(hostname, settings.ExcludePatterns) {
		return false
	}

	// If include patterns exist, must match one
	if len(settings.IncludePatterns) > 0 {
		return matchesPatterns(hostname, settings.IncludePatterns)
	}

	return true
}

// matchesPatterns checks if hostname matches any of the patterns.
func matchesPatterns(hostname string, patterns []HostPattern) bool {
	for _, p := range patterns {
		if p.Kind == "regex" && p.Regex != nil {
			if p.Regex.MatchString(hostname) {
				return true
			}
		} else if p.Kind == "glob" {
			if matchGlob(p.Pattern, hostname) {
				return true
			}
		}
	}
	return false
}

// matchGlob performs simple glob matching (supports * and ?).
func matchGlob(pattern, s string) bool {
	// Simple glob implementation - for full support, use filepath.Match
	// but that doesn't support ** patterns
	matched, _ := filepath.Match(pattern, s)
	return matched
}

// parseHostPatterns parses pattern strings into HostPattern structs.
func parseHostPatterns(patterns []string) []HostPattern {
	var result []HostPattern
	for _, raw := range patterns {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		if strings.HasPrefix(strings.ToLower(raw), "regex:") {
			body := strings.TrimPrefix(raw[6:], "")
			if body == "" {
				slog.Warn("ignoring empty regex pattern in recording config")
				continue
			}
			re, err := regexp.Compile(body)
			if err != nil {
				slog.Warn("invalid regex pattern", "pattern", raw, "err", err)
				continue
			}
			result = append(result, HostPattern{
				Raw:     raw,
				Kind:    "regex",
				Pattern: body,
				Regex:   re,
			})
		} else {
			result = append(result, HostPattern{
				Raw:     raw,
				Kind:    "glob",
				Pattern: raw,
			})
		}
	}
	return result
}

// BuildRecordingPlan creates a plan for recording a session.
// When recording is enabled but prerequisites are missing, it returns an error
// so callers can fail fast instead of silently skipping recording.
func BuildRecordingPlan(hostname string, settings RecordingSettings) (RecordingPlan, error) {
	if !ShouldRecord(hostname, settings) {
		return RecordingPlan{Enabled: false, Reason: "recording disabled by settings"}, nil
	}

	asciinemaPath, err := exec.LookPath("asciinema")
	if err != nil {
		msg := "asciinema binary not found in PATH"
		slog.Debug(msg)
		return RecordingPlan{Enabled: false, Reason: msg, Warn: true}, nil
	}

	now := time.Now()
	sessionDir := sessionDirectory(hostname, settings, now)

	// Ensure directory exists
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		slog.Warn("failed to create session directory", "dir", sessionDir, "err", err)
		return RecordingPlan{}, fmt.Errorf("failed to create session directory: %w", err)
	}

	var sequence int
	var sessionLabel string
	var lockDir string

	if settings.AppendMode {
		// Try to find an unlocked session to append to
		sequence = findUnlockedSession(sessionDir)
		if sequence < 0 {
			// No unlocked session, allocate new one
			sequence = allocateSessionSequence(sessionDir)
		}
	} else {
		sequence = allocateSessionSequence(sessionDir)
	}

	sessionLabel = formatSessionLabel(sequence)
	castPath := filepath.Join(sessionDir, sessionLabel+".cast")
	lockDir = filepath.Join(sessionDir, "."+sessionLabel+".lock")

	// Expand title template
	title := expandTitleTemplate(settings.TitleFormat, hostname, now)

	// Determine if idle time limit applies to recording
	var idleTimeLimit float64
	mode := strings.ToLower(settings.IdleTimeLimitMode)
	if settings.IdleTimeLimit > 0 && (mode == "record" || mode == "both") {
		idleTimeLimit = settings.IdleTimeLimit
	}

	return RecordingPlan{
		Enabled:       true,
		CastPath:      castPath,
		Append:        settings.AppendMode,
		Title:         title,
		AsciinemaPath: asciinemaPath,
		LockDirectory: lockDir,
		Sequence:      sequence,
		SessionLabel:  sessionLabel,
		IdleTimeLimit: idleTimeLimit,
	}, nil
}

// expandTitleTemplate expands a title template with placeholders.
// Supported placeholders: {host}, {user}, {date}, {time}
func expandTitleTemplate(template, hostname string, when time.Time) string {
	if template == "" {
		return fmt.Sprintf("nssh:%s", hostname)
	}

	// Get current user
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("LOGNAME")
	}

	replacer := strings.NewReplacer(
		"{host}", hostname,
		"{user}", username,
		"{date}", when.Format("2006-01-02"),
		"{time}", when.Format("15:04:05"),
	)
	return replacer.Replace(template)
}

// sessionDirectory returns the path for session recordings.
// Format: <recordings_dir>/<sanitized_hostname>/<date>/
func sessionDirectory(hostname string, settings RecordingSettings, when time.Time) string {
	sanitized := sanitizeHostname(hostname)
	datePart := when.Format("2006-01-02")
	return filepath.Join(settings.Directory, sanitized, datePart)
}

// sanitizeHostname makes a hostname safe for use in a filesystem path.
func sanitizeHostname(hostname string) string {
	// Replace problematic characters with underscores
	re := regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	return re.ReplaceAllString(hostname, "_")
}

// formatSessionLabel creates a session label from a sequence number.
func formatSessionLabel(sequence int) string {
	return fmt.Sprintf("session-%03d", sequence)
}

// sessionCounterPath returns the path to the session counter file.
func sessionCounterPath(sessionDir string) string {
	return filepath.Join(sessionDir, ".session-counter")
}

var counterMutex sync.Mutex

// allocateSessionSequence atomically allocates the next session sequence number.
// Uses file-level locking (flock) to ensure cross-process safety.
func allocateSessionSequence(sessionDir string) int {
	// Process-local mutex for in-process safety
	counterMutex.Lock()
	defer counterMutex.Unlock()

	counterPath := sessionCounterPath(sessionDir)
	lockPath := counterPath + ".lock"

	// Acquire cross-process file lock
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		slog.Warn("failed to open counter lock file", "path", lockPath, "err", err)
		// Fall back to process-local mutex only
		return allocateSessionSequenceUnsafe(sessionDir, counterPath)
	}
	defer func() { _ = lockFile.Close() }()

	// Acquire exclusive lock (blocking)
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		slog.Warn("failed to acquire counter file lock", "err", err)
		return allocateSessionSequenceUnsafe(sessionDir, counterPath)
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()

	return allocateSessionSequenceUnsafe(sessionDir, counterPath)
}

// allocateSessionSequenceUnsafe performs the actual sequence allocation.
// Must be called while holding appropriate locks.
func allocateSessionSequenceUnsafe(sessionDir, counterPath string) int {
	current := -1
	if data, err := os.ReadFile(counterPath); err == nil {
		if val, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			current = val
		}
	}

	// Check if current slot is unused and unlocked - reuse it
	if current >= 0 {
		label := formatSessionLabel(current)
		castPath := filepath.Join(sessionDir, label+".cast")
		lockDir := filepath.Join(sessionDir, "."+label+".lock")
		if _, err := os.Stat(castPath); os.IsNotExist(err) {
			if !isSessionLocked(lockDir) {
				return current
			}
		}
	}

	next := current + 1

	// Write atomically
	tmpPath := counterPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(strconv.Itoa(next)), 0600); err != nil {
		slog.Warn("failed to write session counter", "err", err)
		return next
	}
	if err := os.Rename(tmpPath, counterPath); err != nil {
		slog.Debug("failed to rename session counter", "err", err)
	}

	return next
}

// findUnlockedSession finds the most recent unlocked session in the directory.
// Returns -1 if no unlocked session found.
func findUnlockedSession(sessionDir string) int {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return -1
	}

	var sessions []int
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cast") {
			matches := sessionFilePattern.FindStringSubmatch(entry.Name())
			if len(matches) == 2 {
				if num, err := strconv.Atoi(matches[1]); err == nil {
					sessions = append(sessions, num)
				}
			}
		}
	}

	if len(sessions) == 0 {
		return -1
	}

	// Sort descending to check most recent first
	for i := len(sessions) - 1; i >= 0; i-- {
		for j := 0; j < i; j++ {
			if sessions[j] < sessions[j+1] {
				sessions[j], sessions[j+1] = sessions[j+1], sessions[j]
			}
		}
	}

	for _, seq := range sessions {
		label := formatSessionLabel(seq)
		lockDir := filepath.Join(sessionDir, "."+label+".lock")
		if !isSessionLocked(lockDir) {
			return seq
		}
	}

	return -1
}

// isSessionLocked checks if a session lock is held by a live process.
func isSessionLocked(lockDir string) bool {
	info, err := os.Stat(lockDir)
	if err != nil || !info.IsDir() {
		return false
	}

	infoPath := filepath.Join(lockDir, ".lockinfo")
	data, err := os.ReadFile(infoPath)
	if err != nil {
		return false
	}

	// Parse pid=NNN from .lockinfo
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "pid=") {
			pidStr := strings.TrimPrefix(line, "pid=")
			pidStr = strings.TrimSpace(pidStr)
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				return false
			}
			// Check if process exists
			proc, err := os.FindProcess(pid)
			if err != nil {
				return false
			}
			// On Unix, FindProcess always succeeds - send signal 0 to check if process exists
			err = proc.Signal(syscall.Signal(0))
			return err == nil
		}
	}

	return false
}

// SessionLock manages a directory-based lock for recording sessions.
type SessionLock struct {
	dir  string
	held bool
}

// AcquireSessionLock acquires a directory-based lock for recording.
func AcquireSessionLock(lockDir string) (*SessionLock, error) {
	if lockDir == "" {
		return &SessionLock{}, nil
	}

	const maxAttempts = 100
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := os.Mkdir(lockDir, 0700)
		if err == nil {
			// Successfully created - write our PID
			infoPath := filepath.Join(lockDir, ".lockinfo")
			content := fmt.Sprintf("pid=%d\ncmd=nssh\n", os.Getpid())
			if err := os.WriteFile(infoPath, []byte(content), 0600); err != nil {
				slog.Debug("failed to write lock info", "err", err)
			}
			return &SessionLock{dir: lockDir, held: true}, nil
		}

		if os.IsExist(err) {
			// Lock exists - check if stale
			if !isSessionLocked(lockDir) {
				// Stale lock - try to remove it
				// Note: There's an inherent race here between checking staleness and removal.
				// Another process could acquire the lock between our check and removal.
				// We mitigate this by:
				// 1. RemoveAll failing if the directory was recreated with new contents
				// 2. Retrying the Mkdir which will fail if another process won the race
				if err := os.RemoveAll(lockDir); err != nil {
					slog.Debug("failed to remove stale lock", "dir", lockDir, "err", err)
					// Another process may have already cleaned it up or acquired it
				}
				// Small backoff before retry to reduce contention
				time.Sleep(10 * time.Millisecond)
				continue
			}
			// Still locked - wait and retry
			time.Sleep(100 * time.Millisecond)
			continue
		}

		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	return nil, fmt.Errorf("failed to acquire recording lock after %d attempts", maxAttempts)
}

// Release releases the session lock.
func (l *SessionLock) Release() {
	if !l.held || l.dir == "" {
		return
	}

	infoPath := filepath.Join(l.dir, ".lockinfo")
	_ = os.Remove(infoPath)
	_ = os.Remove(l.dir)
	l.held = false
}

// BuildAsciinemaCommand constructs the asciinema command for recording.
func BuildAsciinemaCommand(plan RecordingPlan, sshCmd []string) []string {
	if plan.CastPath == "" || plan.AsciinemaPath == "" {
		return nil
	}

	// Build SSH command as a single string
	var quotedParts []string
	for _, arg := range sshCmd {
		if strings.Contains(arg, " ") || strings.Contains(arg, "'") || strings.Contains(arg, "\"") {
			quotedParts = append(quotedParts, fmt.Sprintf("%q", arg))
		} else {
			quotedParts = append(quotedParts, arg)
		}
	}
	sshCmdStr := strings.Join(quotedParts, " ")

	cmd := []string{plan.AsciinemaPath, "rec", "--quiet"}

	// Headless mode for non-interactive recording
	if os.Getenv("NSSH_RECORD_HEADLESS") == "1" || os.Getenv("NSSH_RECORD_HEADLESS") == "true" {
		cmd = append(cmd, "--headless")
	}

	// Idle time limit
	if plan.IdleTimeLimit > 0 {
		cmd = append(cmd, "--idle-time-limit", fmt.Sprintf("%.1f", plan.IdleTimeLimit))
	}

	// Append mode
	if plan.Append {
		cmd = append(cmd, "--append")
	}

	// Title
	if plan.Title != "" {
		cmd = append(cmd, "--title", plan.Title)
	}

	// Command to record and output file
	cmd = append(cmd, "--command", sshCmdStr, plan.CastPath)

	return cmd
}

// WriteIndex writes or appends session metadata to the index file.
func WriteIndex(castPath, hostname string, startedAt, finishedAt time.Time, exitCode int, authMethod string, sshArgs []string, sessionLabel string) error {
	indexPath := strings.TrimSuffix(castPath, ".cast") + ".index.json"

	// Load existing index
	var payload IndexPayload
	if data, err := os.ReadFile(indexPath); err == nil {
		if err := json.Unmarshal(data, &payload); err != nil {
			slog.Debug("failed to parse existing index", "path", indexPath, "err", err)
		}
	}

	payload.Host = hostname
	payload.Cast = castPath

	entry := IndexEntry{
		Host:       hostname,
		StartedAt:  startedAt.UTC(),
		FinishedAt: finishedAt.UTC(),
		ExitCode:   exitCode,
		Auth:       authMethod,
		Argv:       sshArgs,
		Session:    sessionLabel,
	}
	payload.Sessions = append(payload.Sessions, entry)

	// Write atomically
	tmpPath := indexPath + ".tmp"
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}

	return os.Rename(tmpPath, indexPath)
}

// ExtractSessionLabel extracts the session label from a cast path.
func ExtractSessionLabel(castPath string) string {
	matches := sessionFilePattern.FindStringSubmatch(filepath.Base(castPath))
	if len(matches) == 2 {
		return "session-" + matches[1]
	}
	return ""
}

// ReadCastMetadata reads metadata from an asciinema .cast file.
// Layer 1: Tries .index.json first (instant lookup).
// Layer 2: Falls back to tail-read of .cast file (reads last 64KB instead of full file).
func ReadCastMetadata(castPath string) (*SessionRecord, error) {
	// Layer 1: Try index file first (fastest path)
	if record := readFromIndex(castPath); record != nil {
		return record, nil
	}

	// Layer 2: Fall back to reading cast file with tail optimization
	return readFromCastFile(castPath)
}

// readFromIndex attempts to read session metadata from the .index.json sidecar file.
func readFromIndex(castPath string) *SessionRecord {
	indexPath := strings.TrimSuffix(castPath, ".cast") + ".index.json"

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil
	}

	var payload IndexPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		slog.Debug("failed to parse index file", "path", indexPath, "err", err)
		return nil
	}

	if len(payload.Sessions) == 0 {
		return nil
	}

	// Calculate total duration from all sessions
	var startedAt, finishedAt time.Time
	var argv []string

	for i := range payload.Sessions {
		session := &payload.Sessions[i]
		if i == 0 {
			startedAt = session.StartedAt
			argv = session.Argv
		}
		if session.FinishedAt.After(finishedAt) {
			finishedAt = session.FinishedAt
		}
	}

	return &SessionRecord{
		Host:         payload.Host,
		CastPath:     castPath,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		Argv:         argv,
		SessionLabel: ExtractSessionLabel(castPath),
	}
}

// readFromCastFile reads metadata from cast file header.
// Duration is not available from v3 cast files without reading the entire file
// (v3 uses relative timestamps that must be summed). For accurate duration,
// use the .index.json sidecar file which is written when sessions end.
func readFromCastFile(castPath string) (*SessionRecord, error) {
	f, err := os.Open(castPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	// Read header (first line only - O(1))
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read cast header: %w", err)
		}
		return nil, fmt.Errorf("empty cast file")
	}

	var header struct {
		Timestamp float64 `json:"timestamp"`
		Title     string  `json:"title"`
		Command   string  `json:"command"`
	}

	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return nil, fmt.Errorf("parse cast header: %w", err)
	}

	startedAt := time.Unix(int64(header.Timestamp), 0).UTC()

	// Extract hostname from title (format: "nssh:hostname")
	host := strings.TrimPrefix(header.Title, "nssh:")
	if host == header.Title {
		parts := strings.Split(castPath, string(filepath.Separator))
		if len(parts) >= 3 {
			host = parts[len(parts)-3]
		}
	}

	argv := strings.Fields(header.Command)

	// For files without index, use file mtime as approximate finish time.
	// This is accurate for completed sessions since the file is written during recording.
	finishedAt := startedAt
	if info, err := f.Stat(); err == nil {
		finishedAt = info.ModTime()
	}

	return &SessionRecord{
		Host:         host,
		CastPath:     castPath,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		Argv:         argv,
		SessionLabel: ExtractSessionLabel(castPath),
	}, nil
}

// CastFileInfo holds a cast file path and its modification time for lazy loading.
type CastFileInfo struct {
	Path  string
	Mtime time.Time
}

// ListCastFilesWithMtime returns cast files with modification times (cheap stat only).
// This enables sorting by mtime before loading full metadata.
func ListCastFilesWithMtime(settings RecordingSettings) []CastFileInfo {
	var files []CastFileInfo

	if err := filepath.WalkDir(settings.Directory, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(path, ".cast") {
			info, err := d.Info()
			if err != nil {
				slog.Debug("failed to stat cast file", "path", path, "err", err)
				return nil
			}
			files = append(files, CastFileInfo{Path: path, Mtime: info.ModTime()})
		}
		return nil
	}); err != nil {
		slog.Debug("failed to walk recordings directory", "dir", settings.Directory, "err", err)
	}

	return files
}

// IterSessionRecords returns all session records from the recordings directory.
func IterSessionRecords(settings RecordingSettings) []SessionRecord {
	return IterSessionRecordsLimit(settings, 0)
}

// IterSessionRecordsLimit returns session records, optionally limiting to the N most recent.
// When limit > 0, only loads metadata for the top N files by mtime (lazy loading).
func IterSessionRecordsLimit(settings RecordingSettings, limit int) []SessionRecord {
	// Get files with mtime (cheap - no metadata parsing)
	files := ListCastFilesWithMtime(settings)

	// Sort by mtime descending (newest first)
	sortCastFilesByMtime(files)

	// Apply limit before expensive metadata loading
	if limit > 0 && limit < len(files) {
		files = files[:limit]
	}

	// Now load metadata only for the files we need
	var records []SessionRecord
	for _, f := range files {
		record, err := ReadCastMetadata(f.Path)
		if err != nil {
			slog.Debug("failed to read cast metadata", "path", f.Path, "err", err)
			continue
		}
		records = append(records, *record)
	}

	return records
}

// sortCastFilesByMtime sorts files by modification time descending (newest first).
func sortCastFilesByMtime(files []CastFileInfo) {
	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].Mtime.After(files[i].Mtime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
}

// expandPath expands ~ and environment variables in a path.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	return os.ExpandEnv(path)
}

// ExportToText converts a .cast file to plain text using asciinema convert.
// The output is written to a .txt file alongside the .cast file.
func ExportToText(castPath string) error {
	txtPath := strings.TrimSuffix(castPath, ".cast") + ".txt"
	cmd := exec.Command("asciinema", "convert", "--overwrite", castPath, txtPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("export to text: %w", err)
	}
	slog.Debug("exported recording to text", "cast", castPath, "txt", txtPath)
	return nil
}
