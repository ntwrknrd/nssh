// Package self implements self-management commands for nssh.
package self

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FindBinary returns the full path to the nssh binary, or empty string if not found.
func FindBinary() string {
	// Check PATH first
	if p, err := exec.LookPath("nssh"); err == nil {
		return p
	}

	// Check common locations
	home := homeDir()
	locations := []string{
		filepath.Join(home, ".local", "bin", "nssh"),
		filepath.Join(home, "bin", "nssh"),
		filepath.Join(home, "go", "bin", "nssh"),
		"/opt/homebrew/bin/nssh", // Homebrew on Apple Silicon
		"/usr/local/bin/nssh",    // Homebrew on Intel Mac / manual install
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return ""
}

// FindProjectRoot looks for go.mod starting from cwd and going up.
// Returns the directory containing go.mod, or empty string if not found.
func FindProjectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	dir := cwd
	for i := 0; i < 10; i++ { // Limit search depth
		goMod := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goMod); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return ""
}

// FileExists returns true if the path exists and is a regular file.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// DirExists returns true if the path exists and is a directory.
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// homeDir returns the user's home directory.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

// AbbreviatePath replaces home directory with ~ for display.
func AbbreviatePath(path string) string {
	home := homeDir()
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// Dependency represents an external binary dependency.
type Dependency struct {
	Name     string         // Binary name
	Required bool           // true = required, false = optional
	Purpose  string         // What it's used for
	Path     string         // Resolved path (empty if not found)
	GitHub   *GitHubRelease // GitHub release info (nil if package manager only)
}

// GitHubRelease contains info for downloading from GitHub releases.
type GitHubRelease struct {
	Owner string // e.g., "asciinema"
	Repo  string // e.g., "asciinema"
	// AssetPattern is a function that returns the asset filename for the current platform.
	// Takes (version, goos, goarch) and returns the asset filename.
	AssetPattern func(version, goos, goarch string) string
	// IsArchive indicates if the asset is a tar.gz archive that needs extraction.
	IsArchive bool
	// BinaryName is the name of the binary inside the archive (if different from dep name).
	BinaryName string
}

// asciinemaAssetPattern returns the GitHub release asset filename for asciinema.
func asciinemaAssetPattern(version, goos, goarch string) string {
	arch := "x86_64"
	if goarch == "arm64" {
		arch = "aarch64"
	}

	os := "unknown-linux-gnu"
	if goos == "darwin" {
		os = "apple-darwin"
	}

	return fmt.Sprintf("asciinema-%s-%s", arch, os)
}

// aggAssetPattern returns the GitHub release asset filename for agg.
func aggAssetPattern(version, goos, goarch string) string {
	arch := "x86_64"
	if goarch == "arm64" {
		arch = "aarch64"
	}

	os := "unknown-linux-gnu"
	if goos == "darwin" {
		os = "apple-darwin"
	}

	return fmt.Sprintf("agg-%s-%s", arch, os)
}

// fzfAssetPattern returns the GitHub release asset filename for fzf.
func fzfAssetPattern(version, goos, goarch string) string {
	// fzf uses format: fzf-{version}-{os}_{arch}.tar.gz
	// version comes with 'v' prefix, but filename uses without
	ver := strings.TrimPrefix(version, "v")
	return fmt.Sprintf("fzf-%s-%s_%s.tar.gz", ver, goos, goarch)
}

// Dependencies returns the list of external dependencies.
func Dependencies() []Dependency {
	deps := []Dependency{
		{Name: "ssh", Required: true, Purpose: "SSH connections"},
		{Name: "scp", Required: true, Purpose: "file transfers"},
		{
			Name:     "asciinema",
			Required: false,
			Purpose:  "session recording",
			GitHub: &GitHubRelease{
				Owner:        "asciinema",
				Repo:         "asciinema",
				AssetPattern: asciinemaAssetPattern,
			},
		},
		{
			Name:     "agg",
			Required: false,
			Purpose:  "GIF export",
			GitHub: &GitHubRelease{
				Owner:        "asciinema",
				Repo:         "agg",
				AssetPattern: aggAssetPattern,
			},
		},
		{
			Name:     "fzf",
			Required: false,
			Purpose:  "fuzzy selection",
			GitHub: &GitHubRelease{
				Owner:        "junegunn",
				Repo:         "fzf",
				AssetPattern: fzfAssetPattern,
				IsArchive:    true,
				BinaryName:   "fzf",
			},
		},
	}

	// Resolve paths
	for i := range deps {
		if path, err := exec.LookPath(deps[i].Name); err == nil {
			deps[i].Path = path
		}
	}

	return deps
}

// InstalledDependencyPaths returns paths to dependencies installed by nssh in ~/.local/bin.
// Only returns paths that actually exist.
func InstalledDependencyPaths() []string {
	installDir := filepath.Join(homeDir(), ".local", "bin")
	var paths []string

	for _, dep := range Dependencies() {
		if dep.GitHub == nil {
			continue // Only track GitHub-installed deps
		}
		binPath := filepath.Join(installDir, dep.Name)
		if FileExists(binPath) {
			paths = append(paths, binPath)
		}
	}

	return paths
}
