// Package cli provides CLI subcommand implementations and shared utilities.
package cli

// HostNotFoundError indicates a host was not found and carries the hostname
// for potential use in spawning local inventory creation.
type HostNotFoundError struct {
	Hostname string
}

func (e *HostNotFoundError) Error() string {
	return "host not found: " + e.Hostname
}
