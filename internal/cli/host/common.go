// Package host provides CLI commands for SSH host management.
package host

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
)

// Cached config to avoid repeated disk reads and TOML parsing.
var (
	cachedConfig     *config.Config
	cachedConfigOnce sync.Once
)

// getCachedConfig returns a cached config, loading it once on first call.
func getCachedConfig() *config.Config {
	cachedConfigOnce.Do(func() {
		cfg, err := config.LoadDefault()
		if err != nil {
			cfg = config.DefaultConfig()
		}
		cachedConfig = cfg
	})
	return cachedConfig
}

// abbreviateHome replaces home directory with ~
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}

	return path
}

// authTypeString returns a human-readable auth type.
func authTypeString(host *sshconfig.HostEntry) string {
	if host.UsesPassword() {
		return "password"
	}
	return "key"
}

// createBackup creates a timestamped backup of a file.
func createBackup(srcPath, backupDir string, maxBackups int) error {
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return nil // Nothing to backup
	}

	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("%s.%s.bak", filepath.Base(srcPath), timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	// Copy file
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy to backup: %w", err)
	}

	// Prune old backups
	pruneBackups(backupDir, filepath.Base(srcPath), maxBackups)
	return nil
}

// pruneBackups removes old backups exceeding the limit.
func pruneBackups(backupDir, sourcePrefix string, maxBackups int) {
	if maxBackups <= 0 {
		return
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return // Ignore if dir doesn't exist
	}

	// Filter matching backups
	var backups []os.DirEntry
	pattern := sourcePrefix + "."
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), pattern) && strings.HasSuffix(e.Name(), ".bak") {
			backups = append(backups, e)
		}
	}

	if len(backups) <= maxBackups {
		return
	}

	// Sort by modification time (newest first) using info
	type backupInfo struct {
		entry os.DirEntry
		mtime time.Time
	}
	var infos []backupInfo
	for _, b := range backups {
		if fi, err := b.Info(); err == nil {
			infos = append(infos, backupInfo{b, fi.ModTime()})
		}
	}

	// Sort newest first
	for i := 0; i < len(infos)-1; i++ {
		for j := i + 1; j < len(infos); j++ {
			if infos[i].mtime.Before(infos[j].mtime) {
				infos[i], infos[j] = infos[j], infos[i]
			}
		}
	}

	// Remove oldest
	for i := maxBackups; i < len(infos); i++ {
		_ = os.Remove(filepath.Join(backupDir, infos[i].entry.Name()))
	}
}

// getParser creates a parser with default paths.
func getParser() *sshconfig.Parser {
	return sshconfig.NewParser()
}

// getBackupDir returns the backup directory path.
func getBackupDir() string {
	return config.DefaultPaths().BackupDir
}

// getMaxBackups returns the max backups setting.
func getMaxBackups() int {
	return getCachedConfig().Logging.Audit.MaxBackupFiles
}

// getDefaultContext returns the default context from config.
func getDefaultContext() string {
	return getCachedConfig().Host.Defaults.DefaultContext
}

// getDefaultUser returns the default user from config.
func getDefaultUser() string {
	return getCachedConfig().Host.Defaults.DefaultUser
}

// newSecretFromString creates a secret from a string.
func newSecretFromString(pw string) *secret.Secret {
	return secret.New([]byte(pw))
}

// maskPassword returns a deterministic asterisk mask based on password hash.
// Length is 6-13 asterisks based on hash, not actual password length.
func maskPassword(password string) string {
	if password == "" {
		return "(no password)"
	}
	hash := sha256.Sum256([]byte(password))
	length := int(hash[0]%8) + 6
	result := make([]byte, length)
	for i := range result {
		result[i] = '*'
	}
	return string(result)
}

// displayHostDetails shows host context, credentials, and SSH config.
// Used by both 'host get' and 'host edit' commands.
// Returns the context name for audit logging purposes.
func displayHostDetails(hostName string, hostLines []string, configPath string, vm *vault.Manager, showSecret bool) string {
	contextName := "-"
	var ctxCred *vault.Credential
	var hostCreds []vault.Credential

	if vm != nil {
		// Check for context credentials
		configBase := filepath.Base(configPath)
		ctx, _ := vm.GetContextByIncludeFile(configBase)
		if ctx != nil && ctx.Credential != nil {
			contextName = ctx.Name
			ctxCred = ctx.Credential
		}

		// Check for host-specific credentials
		hostCreds, _ = vm.GetHostCredentials(hostName)
	}

	// Display metadata
	fmt.Println()
	ui.PrintKeyValue("Context", contextName)

	// Determine which credential is the effective default
	hasHostDefault := false
	for _, cred := range hostCreds {
		if cred.Default {
			hasHostDefault = true
			break
		}
	}

	// Display all credentials as a list
	ui.PrintKeyValue("Credentials", "")

	// Show host-specific credentials
	for _, cred := range hostCreds {
		passwordDisplay := maskPassword(cred.Password)
		if showSecret {
			passwordDisplay = cred.Password
		}
		prefix := "      "
		suffix := " (host)"
		if cred.Default {
			prefix = "    > "
		}
		fmt.Printf("%s%s / %s%s\n", prefix, cred.Username, passwordDisplay, suffix)
	}

	// Show context credential
	if ctxCred != nil {
		passwordDisplay := maskPassword(ctxCred.Password)
		if showSecret {
			passwordDisplay = ctxCred.Password
		}
		prefix := "      "
		suffix := " (context)"
		if !hasHostDefault {
			prefix = "    > "
		}
		fmt.Printf("%s%s / %s%s\n", prefix, ctxCred.Username, passwordDisplay, suffix)
	}

	// No credentials at all
	if len(hostCreds) == 0 && ctxCred == nil {
		fmt.Println("      (none)")
	}

	// Show SSH config in a bordered panel
	var configLines strings.Builder
	for _, line := range hostLines {
		configLines.WriteString(line)
	}
	fmt.Println()
	fmt.Println(ui.BorderedPanel(abbreviateHome(configPath), strings.TrimRight(configLines.String(), "\n"), "center", 0, true))

	return contextName
}
