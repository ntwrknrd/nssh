// Package config provides application configuration loading and path resolution.
package config

import (
	"os"
	"path/filepath"
	"sync"
)

// Paths holds resolved XDG-compliant paths for nssh.
type Paths struct {
	// ConfigDir is XDG_CONFIG_HOME/nssh (default: ~/.config/nssh)
	ConfigDir string

	// DataDir is XDG_DATA_HOME/nssh (default: ~/.local/share/nssh)
	DataDir string

	// StateDir is XDG_STATE_HOME/nssh (default: ~/.local/state/nssh)
	StateDir string

	// ConfigFile is the main config file path
	ConfigFile string

	// RecordingsDir is where session recordings are stored (state, not data)
	RecordingsDir string

	// BackupDir is where local config and inventory backups are stored
	BackupDir string

	// SSHConfigDir is the user's SSH config directory
	SSHConfigDir string

	// SSHConfigFile is the main SSH config file
	SSHConfigFile string
}

var (
	defaultPaths *Paths
	pathsOnce    sync.Once
)

// DefaultPaths returns the default XDG-compliant paths.
// The result is cached after first call.
func DefaultPaths() *Paths {
	pathsOnce.Do(func() {
		defaultPaths = resolvePaths()
	})
	return defaultPaths
}

// resolvePaths computes all paths based on XDG spec and environment.
func resolvePaths() *Paths {
	home := homeDir()

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}

	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}

	configDir := filepath.Join(configHome, "nssh")
	dataDir := filepath.Join(dataHome, "nssh")
	stateDir := filepath.Join(stateHome, "nssh")
	sshDir := filepath.Join(home, ".ssh")

	return &Paths{
		ConfigDir:     configDir,
		DataDir:       dataDir,
		StateDir:      stateDir,
		ConfigFile:    filepath.Join(configDir, "config.yaml"),
		RecordingsDir: filepath.Join(stateDir, "casts"), // State, matches Python
		BackupDir:     filepath.Join(dataDir, "backups"),
		SSHConfigDir:  sshDir,
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
}

// EnsureDirs creates all required directories with appropriate permissions.
func (p *Paths) EnsureDirs() error {
	dirs := []string{
		p.ConfigDir,
		p.DataDir,
		p.StateDir,
		p.RecordingsDir,
		p.BackupDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

// homeDir returns the user's home directory.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	// Fallback to HOME env var
	return os.Getenv("HOME")
}
