// Package config provides configuration loading and path resolution for nssh.
//
// Configuration is loaded from a TOML file with environment variable overrides.
// The package follows XDG Base Directory conventions for file placement.
//
// # Configuration File
//
// The configuration file is located at:
//
//	$XDG_CONFIG_HOME/nssh/config.toml  (default: ~/.config/nssh/config.toml)
//
// Use [LoadDefault] to load configuration with standard path resolution,
// or [Load] to load from a specific path.
//
// # Environment Variables
//
// Configuration values can be overridden via environment variables using the
// NSSH_ prefix. Nested keys use underscores:
//
//	NSSH_AGENT_IDLE_TIMEOUT=2h
//	NSSH_SSH_CONNECTION_TIMEOUT=30s
//	NSSH_LOGGING_SESSION_ENABLED=true
//
// # Path Resolution
//
// The [Paths] struct provides XDG-compliant path resolution:
//
//	Config:     $XDG_CONFIG_HOME/nssh     (~/.config/nssh)
//	Data:       $XDG_DATA_HOME/nssh       (~/.local/share/nssh)
//	State:      $XDG_STATE_HOME/nssh      (~/.local/state/nssh)
//	Recordings: $XDG_STATE_HOME/nssh/recordings
//
// Use [DefaultPaths] to get resolved paths for the current environment.
//
// # Duration Type
//
// The [Duration] type wraps [time.Duration] for TOML parsing, accepting
// standard Go duration strings like "30s", "5m", "1h".
package config
