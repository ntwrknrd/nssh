package connect

import (
	"fmt"
	"log/slog"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// ResolveHostname performs smart hostname resolution:
// - Exact match: returns hostname unchanged
// - Single partial match: returns the matched hostname
// - Multiple partial matches: opens fuzzy finder with query pre-filled
// - No matches: returns HostNotFoundError to trigger local inventory creation
func ResolveHostname(hostname string) (string, error) {
	cfg, err := config.LoadDefault()
	if err != nil {
		return "", err
	}
	catalog, err := BuildHostCatalog(cfg)
	if err != nil {
		return "", err
	}
	if host, ok := catalog.Find(hostname); ok {
		if host.Canonical != hostname {
			slog.Debug("auto-resolved hostname", "input", hostname, "resolved", host.Canonical)
		}
		return host.Canonical, nil
	}
	suggestions := catalog.Suggestions(hostname)
	if len(suggestions) > 0 {
		selected, err := ui.FuzzySelectString("Select host", suggestions, hostname)
		if err != nil {
			return "", fmt.Errorf("fuzzy select: %w", err)
		}
		if selected == "" {
			return "", fmt.Errorf("selection canceled")
		}
		return selected, nil
	}

	if hostname == "" {
		return "", fmt.Errorf("no hosts found in nssh inventory")
	}
	return "", &HostNotFoundError{Hostname: hostname}
}
