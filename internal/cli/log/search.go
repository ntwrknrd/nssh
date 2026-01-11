package log

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/recording"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewSearchCmd creates the 'log search' command.
func NewSearchCmd() *cobra.Command {
	var (
		selectPattern string
		lastN         int
		caseSensitive bool
		context       int
	)

	cmd := &cobra.Command{
		Use:   "search <pattern>",
		Short: "Search recordings for text",
		Long: `Search through session recordings for a keyword or pattern.

Searches the terminal output captured in .cast files. If .txt exports exist
alongside .cast files, those are searched instead (faster).

Examples:
  nssh log search "show interfaces"
  nssh log search -s router1 "bgp neighbor"
  nssh log search --last 10 "error"
  nssh log search -i "WARNING"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(args[0], selectPattern, lastN, caseSensitive, context)
		},
	}

	cmd.Flags().StringVarP(&selectPattern, "select", "s", "", "Filter sessions by pattern (today, yesterday, this-week, this-month, or regex)")
	cmd.Flags().IntVarP(&lastN, "last", "l", 0, "Search only the last N sessions")
	cmd.Flags().BoolVarP(&caseSensitive, "case-sensitive", "i", false, "Case-sensitive search")
	cmd.Flags().IntVarP(&context, "context", "C", 0, "Show N lines of context around matches")

	return cmd
}

// searchMatch represents a single match in a recording.
type searchMatch struct {
	Session    recording.SessionRecord
	LineNumber int
	Line       string
}

func runSearch(pattern, selectPattern string, lastN int, caseSensitive bool, contextLines int) error {
	settings := recording.LoadRecordingSettings()
	localTZ := time.Now().Location()

	ui.CommandStart("SEARCH RECORDINGS")

	var sessions []recording.SessionRecord
	if lastN > 0 && selectPattern == "" {
		sessions = LoadSessionsLimit(settings, lastN)
	} else {
		sessions = LoadSessions(settings)
	}

	if selectPattern != "" {
		selectPattern = ExpandDateShortcut(selectPattern)
		re, err := regexp.Compile("(?i)" + selectPattern)
		if err != nil {
			ui.Error("Invalid regex pattern: %s", err)
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1}
		}

		var filtered []recording.SessionRecord
		for _, s := range sessions {
			startDate := s.StartedAt.In(localTZ).Format("2006-01-02")
			mtimeDate := sessionUpdatedTimestamp(s).In(localTZ).Format("2006-01-02")
			if MatchesPattern(re, s.Host, s.SessionLabel, startDate, mtimeDate) {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered

		if lastN > 0 && lastN < len(sessions) {
			sessions = sessions[:lastN]
		}
	}

	if len(sessions) == 0 {
		ui.Warning("No sessions to search")
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	ui.Info("Searching %d session(s) for: %s", len(sessions), pattern)

	var searchRe *regexp.Regexp
	var err error
	if caseSensitive {
		searchRe, err = regexp.Compile(pattern)
	} else {
		searchRe, err = regexp.Compile("(?i)" + pattern)
	}
	if err != nil {
		ui.Error("Invalid search pattern: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: 1}
	}

	var allMatches []searchMatch
	sessionsWithMatches := 0

	for _, session := range sessions {
		matches, err := searchSession(session, searchRe, contextLines)
		if err != nil {
			continue
		}
		if len(matches) > 0 {
			allMatches = append(allMatches, matches...)
			sessionsWithMatches++
		}
	}

	if len(allMatches) == 0 {
		ui.Warning("No matches found for: %s", pattern)
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	printSearchResults(allMatches, searchRe, localTZ)

	ui.Info("Found %d match(es) in %d session(s)", len(allMatches), sessionsWithMatches)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func searchSession(session recording.SessionRecord, pattern *regexp.Regexp, contextLines int) ([]searchMatch, error) {
	txtPath := strings.TrimSuffix(session.CastPath, ".cast") + ".txt"
	if _, err := os.Stat(txtPath); err == nil {
		return searchTextFile(session, txtPath, pattern, contextLines)
	}
	return searchCastFile(session, pattern, contextLines)
}

func searchTextFile(session recording.SessionRecord, txtPath string, pattern *regexp.Regexp, contextLines int) ([]searchMatch, error) {
	f, err := os.Open(txtPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var matches []searchMatch
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	for i, line := range lines {
		if pattern.MatchString(line) {
			if contextLines > 0 {
				start := i - contextLines
				if start < 0 {
					start = 0
				}
				end := i + contextLines + 1
				if end > len(lines) {
					end = len(lines)
				}
				contextStr := strings.Join(lines[start:end], "\n")
				matches = append(matches, searchMatch{
					Session:    session,
					LineNumber: i + 1,
					Line:       contextStr,
				})
			} else {
				matches = append(matches, searchMatch{
					Session:    session,
					LineNumber: i + 1,
					Line:       line,
				})
			}
		}
	}

	return matches, nil
}

func searchCastFile(session recording.SessionRecord, pattern *regexp.Regexp, contextLines int) ([]searchMatch, error) {
	f, err := os.Open(session.CastPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	if !scanner.Scan() {
		return nil, nil
	}

	var outputChunks []string
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event []any
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		if len(event) >= 3 {
			if eventType, ok := event[1].(string); ok && eventType == "o" {
				if text, ok := event[2].(string); ok {
					outputChunks = append(outputChunks, text)
				}
			}
		}
	}

	fullOutput := strings.Join(outputChunks, "")
	fullOutput = stripANSI(fullOutput)
	lines := strings.Split(fullOutput, "\n")

	var matches []searchMatch
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if pattern.MatchString(line) {
			if contextLines > 0 {
				start := i - contextLines
				if start < 0 {
					start = 0
				}
				end := i + contextLines + 1
				if end > len(lines) {
					end = len(lines)
				}
				var contextParts []string
				for j := start; j < end; j++ {
					if l := strings.TrimSpace(lines[j]); l != "" {
						contextParts = append(contextParts, l)
					}
				}
				matches = append(matches, searchMatch{
					Session:    session,
					LineNumber: i + 1,
					Line:       strings.Join(contextParts, "\n"),
				})
			} else {
				matches = append(matches, searchMatch{
					Session:    session,
					LineNumber: i + 1,
					Line:       line,
				})
			}
		}
	}

	return matches, nil
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\\|\x1b\[[\?]?[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func printSearchResults(matches []searchMatch, pattern *regexp.Regexp, tz *time.Location) {
	currentSession := ""

	for i := range matches {
		m := &matches[i]
		sessionKey := m.Session.CastPath
		if sessionKey != currentSession {
			currentSession = sessionKey
			dateStr := m.Session.StartedAt.In(tz).Format("2006-01-02 15:04")
			fmt.Println()
			ui.Info("%s [%s] %s", m.Session.Host, dateStr, homeReplace(m.Session.CastPath))
		}

		lines := strings.Split(m.Line, "\n")
		for _, line := range lines {
			highlighted := pattern.ReplaceAllStringFunc(line, func(match string) string {
				return fmt.Sprintf("\033[1;33m%s\033[0m", match)
			})
			fmt.Printf("  %4d: %s\n", m.LineNumber, highlighted)
		}
	}
	fmt.Println()
}
