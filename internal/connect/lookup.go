package connect

import (
	"fmt"
	"log/slog"
	"sort"

	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// ResolveHostname performs smart hostname resolution:
// - Exact match: returns hostname unchanged
// - Single partial match: returns the matched hostname
// - Multiple partial matches: opens fuzzy finder with query pre-filled
// - No matches: returns HostNotFoundError to trigger local inventory creation
func ResolveHostname(hostname string) (string, error) {
	parser := sshconfig.NewParser()

	result, err := parser.MatchHost(hostname)
	if err != nil {
		slog.Debug("failed to match host", "err", err)
		return hostname, nil
	}

	if result.Host != nil {
		if result.Host.Host != hostname {
			slog.Debug("auto-resolved hostname", "input", hostname, "resolved", result.Host.Host)
		}
		return result.Host.Host, nil
	}

	if len(result.Suggestions) > 0 {
		sort.Strings(result.Suggestions)
		selected, err := ui.FuzzySelectString("Select host", result.Suggestions, hostname)
		if err != nil {
			return "", fmt.Errorf("fuzzy select: %w", err)
		}
		if selected == "" {
			return "", fmt.Errorf("selection canceled")
		}
		return selected, nil
	}

	if hostname == "" {
		return "", fmt.Errorf("no hosts found in SSH config")
	}
	return "", &HostNotFoundError{Hostname: hostname}
}
