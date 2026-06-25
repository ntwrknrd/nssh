package connect

import (
	"fmt"
	"log/slog"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ui"
)

type hostSelector func(prompt string, options []string, initialQuery string) (string, error)

// ResolveHostname performs smart hostname resolution:
// - Exact match: returns hostname unchanged
// - Single partial match: returns the matched hostname
// - Multiple partial matches: opens fuzzy finder with query pre-filled
// - No matches: returns HostNotFoundError to trigger local inventory creation
func ResolveHostname(hostname string) (string, error) {
	configTimer := connector.StartTiming(connector.TimingConfigLoad)
	cfg, err := config.LoadDefault()
	configTimer.Emit()
	if err != nil {
		return "", err
	}
	catalog, err := BuildHostCatalog(cfg)
	if err != nil {
		return "", err
	}
	return resolveHostnameFromCatalog(hostname, catalog, func(prompt string, options []string, initialQuery string) (string, error) {
		return ui.FuzzySelectString(prompt, options, initialQuery)
	})
}

func resolveHostnameFromCatalog(hostname string, catalog *HostCatalog, selectHost hostSelector) (string, error) {
	if host, ok := catalog.Find(hostname); ok {
		if host.Canonical != hostname {
			slog.Debug("auto-resolved hostname", "input", hostname, "resolved", host.Canonical)
		}
		return host.Canonical, nil
	}
	suggestions := catalog.Suggestions(hostname)
	switch len(suggestions) {
	case 0:
	case 1:
		slog.Debug("auto-resolved hostname", "input", hostname, "resolved", suggestions[0])
		return suggestions[0], nil
	default:
		selected, err := selectHost("Select host", suggestions, hostname)
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
