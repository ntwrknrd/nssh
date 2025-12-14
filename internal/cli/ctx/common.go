package ctx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// newSecretFromString creates a secret from a string.
// This is a convenience wrapper for interactive password input.
func newSecretFromString(pw string) *secret.Secret {
	return secret.New([]byte(pw))
}

// selectSSHConfigFile shows a selector for existing SSH config files in conf.d,
// with an option to create a new one. Returns the selected/created filename.
// excludeFiles optionally lists files to omit (e.g., files already used by other contexts).
func selectSSHConfigFile(defaultName string, excludeFiles ...string) (string, error) {
	paths := config.DefaultPaths()
	confD := filepath.Join(paths.SSHConfigDir, "conf.d")

	// Build exclude set
	excludeSet := make(map[string]bool)
	for _, f := range excludeFiles {
		excludeSet[f] = true
	}

	// List existing files in conf.d
	var existingFiles []string
	entries, err := os.ReadDir(confD)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && !excludeSet[entry.Name()] {
				existingFiles = append(existingFiles, entry.Name())
			}
		}
	}

	// Abbreviate conf.d path for display
	confDDisplay := confD
	if home, err := os.UserHomeDir(); err == nil {
		confDDisplay = strings.Replace(confD, home, "~", 1)
	}

	// If no existing files available, go straight to filename input
	if len(existingFiles) == 0 {
		prompt := fmt.Sprintf("SSH config file (%s)", confDDisplay)
		name, err := ui.InputWithDefault(prompt, defaultName)
		if err != nil {
			return "", err
		}
		if name == "" {
			name = defaultName
		}
		return name, nil
	}

	// Build options: existing files + create new
	options := make([]ui.SelectOption, 0, len(existingFiles)+1)

	// Add "Create new" option first with default name hint
	createLabel := "Create new..."
	if defaultName != "" {
		createLabel = "Create new (" + defaultName + ")"
	}
	options = append(options, ui.SelectOption{Label: createLabel, Value: "_new_"})

	// Add existing files
	for _, f := range existingFiles {
		options = append(options, ui.SelectOption{Label: f, Value: f})
	}

	selectPrompt := fmt.Sprintf("SSH config file (%s)", confDDisplay)
	choice, err := ui.Select(selectPrompt, options)
	if err != nil {
		return "", err
	}

	if choice == "_new_" {
		// Prompt for new filename
		name, err := ui.InputWithDefault("New filename", defaultName)
		if err != nil {
			return "", err
		}
		if name == "" {
			name = defaultName
		}
		return name, nil
	}

	return choice, nil
}

// sshConfigPath returns the full path to an SSH config file in conf.d.
func sshConfigPath(filename string) string {
	paths := config.DefaultPaths()
	return filepath.Join(paths.SSHConfigDir, "conf.d", filename)
}

// countHostsInFile counts the number of Host entries in an SSH config file.
// Returns 0 if the file doesn't exist or can't be parsed.
func countHostsInFile(filename string) int {
	path := sshConfigPath(filename)
	parser := sshconfig.NewParser()
	cfg, err := parser.ParseFile(path)
	if err != nil {
		return 0
	}
	return len(cfg.Hosts)
}

// deleteSSHConfigFile deletes an SSH config file from conf.d.
func deleteSSHConfigFile(filename string) error {
	path := sshConfigPath(filename)
	return os.Remove(path)
}
