// Package self implements self-management commands for nssh.
package self

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// rootCmd holds a reference to the root command for completion generation.
var rootCmd *cobra.Command

// SetRootCmd sets the root command (called from main for completion generation).
func SetRootCmd(cmd *cobra.Command) {
	rootCmd = cmd
}

// ShellInfo contains detected shell information.
type ShellInfo struct {
	Name   string // "bash", "zsh", "fish", or "unknown"
	RCFile string // Path to the shell's rc file
}

// DetectShell detects the user's shell and returns appropriate rc file path.
func DetectShell() ShellInfo {
	shell := os.Getenv("SHELL")
	home := homeDir()

	switch {
	case strings.Contains(shell, "fish"):
		return ShellInfo{
			Name:   "fish",
			RCFile: filepath.Join(home, ".config", "fish", "config.fish"),
		}
	case strings.Contains(shell, "zsh"):
		return ShellInfo{
			Name:   "zsh",
			RCFile: filepath.Join(home, ".zshrc"),
		}
	case strings.Contains(shell, "bash"):
		// Prefer .bashrc on Linux, .bash_profile on macOS
		rcFile := filepath.Join(home, ".bashrc")
		if runtime.GOOS == "darwin" {
			rcFile = filepath.Join(home, ".bash_profile")
		}
		return ShellInfo{
			Name:   "bash",
			RCFile: rcFile,
		}
	default:
		return ShellInfo{
			Name:   "unknown",
			RCFile: filepath.Join(home, ".bashrc"),
		}
	}
}

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

// ShellIntegrationMarker is the comment marker used to identify nssh shell integration.
const ShellIntegrationMarker = "# nssh shell integration"

// Dependency represents an external binary dependency.
type Dependency struct {
	Name     string // Binary name
	Required bool   // true = required, false = optional
	Purpose  string // What it's used for
	Path     string // Resolved path (empty if not found)
}

// Dependencies returns the list of external dependencies.
func Dependencies() []Dependency {
	deps := []Dependency{
		{Name: "ssh", Required: true, Purpose: "SSH connections"},
		{Name: "scp", Required: true, Purpose: "file transfers"},
		{Name: "asciinema", Required: false, Purpose: "session recording"},
		{Name: "agg", Required: false, Purpose: "GIF export"},
		{Name: "fzf", Required: false, Purpose: "fuzzy selection"},
	}

	// Resolve paths
	for i := range deps {
		if path, err := exec.LookPath(deps[i].Name); err == nil {
			deps[i].Path = path
		}
	}

	return deps
}

// CheckDependencies returns (allRequired, allOptional) booleans.
func CheckDependencies() (bool, bool) {
	deps := Dependencies()
	allRequired := true
	allOptional := true

	for _, dep := range deps {
		if dep.Required && dep.Path == "" {
			allRequired = false
		}
		if !dep.Required && dep.Path == "" {
			allOptional = false
		}
	}

	return allRequired, allOptional
}

// PackageManager represents a system package manager.
type PackageManager struct {
	Name       string   // e.g., "brew", "apt", "dnf", "pacman"
	InstallCmd []string // e.g., ["brew", "install"]
	Path       string   // resolved path to the package manager
}

// DetectPackageManager finds an available package manager.
func DetectPackageManager() *PackageManager {
	managers := []struct {
		name       string
		binary     string
		installCmd []string
	}{
		// macOS
		{"brew", "brew", []string{"brew", "install"}},
		// Debian/Ubuntu
		{"apt", "apt-get", []string{"sudo", "apt-get", "install", "-y"}},
		// Fedora/RHEL
		{"dnf", "dnf", []string{"sudo", "dnf", "install", "-y"}},
		// Arch
		{"pacman", "pacman", []string{"sudo", "pacman", "-S", "--noconfirm"}},
	}

	for _, m := range managers {
		if path, err := exec.LookPath(m.binary); err == nil {
			return &PackageManager{
				Name:       m.name,
				InstallCmd: m.installCmd,
				Path:       path,
			}
		}
	}

	return nil
}

// PackageName returns the package name for a dependency on a given package manager.
func (d Dependency) PackageName(pm *PackageManager) string {
	// Map dependency binary names to package names
	packages := map[string]map[string]string{
		"ssh": {
			"brew":   "openssh",
			"apt":    "openssh-client",
			"dnf":    "openssh-clients",
			"pacman": "openssh",
		},
		"scp": {
			// scp comes with the same package as ssh
			"brew":   "openssh",
			"apt":    "openssh-client",
			"dnf":    "openssh-clients",
			"pacman": "openssh",
		},
		"asciinema": {
			"brew":   "asciinema",
			"apt":    "asciinema",
			"dnf":    "asciinema",
			"pacman": "asciinema",
		},
		"agg": {
			"brew":   "agg",
			"pacman": "agg",
			// apt/dnf: not available, use cargo install agg or npm install -g asciicast2gif
		},
		"fzf": {
			"brew":   "fzf",
			"apt":    "fzf",
			"dnf":    "fzf",
			"pacman": "fzf",
		},
	}

	if pkgMap, ok := packages[d.Name]; ok {
		if pkg, ok := pkgMap[pm.Name]; ok {
			return pkg
		}
	}

	// Default: use the binary name as package name
	return d.Name
}

// InstallCommand returns the full command to install a package.
func (pm *PackageManager) InstallCommand(packages ...string) []string {
	return append(pm.InstallCmd, packages...)
}
