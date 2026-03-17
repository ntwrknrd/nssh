package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
)

// sshConfigHeader is the header written to sync-owned include files.
const sshConfigHeader = "# Managed by nssh sync -- DO NOT EDIT"

// WriteManagedSSHConfig generates and atomically writes a sync-owned SSH config
// include file as a complete replacement (whole-file atomic rewrite).
func WriteManagedSSHConfig(includeFile string, hosts []*ManagedHost, sourceName, provider string) error {
	paths := config.DefaultPaths()
	content := generateSSHConfig(hosts, sourceName, provider)
	destPath := filepath.Join(paths.SSHConfigDir, includeFile)
	if err := atomicWriteFile(destPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write %s: %w", includeFile, err)
	}
	return nil
}

// RemoveManagedSSHConfig removes a sync-owned include file if it exists.
func RemoveManagedSSHConfig(includeFile string) error {
	paths := config.DefaultPaths()
	destPath := filepath.Join(paths.SSHConfigDir, includeFile)
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", includeFile, err)
	}
	return nil
}

// generateSSHConfig produces the content of a sync-owned include file.
func generateSSHConfig(hosts []*ManagedHost, sourceName, provider string) string {
	// Sort hosts by primary alias for deterministic output
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Host < hosts[j].Host
	})

	var b strings.Builder
	b.WriteString(sshConfigHeader)
	b.WriteByte('\n')
	fmt.Fprintf(&b, "# Source: %s (%s)\n", sourceName, provider)

	for _, h := range hosts {
		b.WriteByte('\n')

		// Host line with all patterns
		patterns := h.Patterns
		if len(patterns) == 0 {
			patterns = []string{h.Host}
		}
		fmt.Fprintf(&b, "Host %s\n", strings.Join(patterns, " "))

		// HostName directive
		fmt.Fprintf(&b, "  HostName %s\n", h.HostName)

		// User directive when known so sync-owned entries look like normal hosts.
		if h.Username != "" {
			fmt.Fprintf(&b, "  User %s\n", h.Username)
		}

		// Port (only if non-default)
		if h.Port > 0 && h.Port != 22 {
			fmt.Fprintf(&b, "  Port %d\n", h.Port)
		}

		// Compatibility directives persisted in sync state.
		for _, ct := range h.CompatFixes {
			cfg, ok := compat.CompatConfigs[ct]
			if !ok {
				continue
			}
			for _, line := range cfg.ConfigLines {
				b.WriteString(line)
			}
		}

		// ProxyJump
		if h.ProxyJump != "" {
			fmt.Fprintf(&b, "  ProxyJump %s\n", h.ProxyJump)
		}

		// Password auth directives for hosts that use passwords
		if h.UsesPassword {
			b.WriteString("  PubkeyAuthentication no\n")
			b.WriteString("  PreferredAuthentications keyboard-interactive,password\n")
		} else {
			b.WriteString("  PubkeyAuthentication yes\n")
			b.WriteString("  PasswordAuthentication no\n")
		}
	}

	return b.String()
}

// atomicWriteFile writes data to path via temp file + rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".sync-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	tmpPath = "" // prevent cleanup
	return nil
}
