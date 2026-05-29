package inventory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
)

const providerSSHConfigHeader = "# Managed by nssh inventory provider -- DO NOT EDIT"

// WriteProviderSSHConfig renders and atomically writes a provider-owned file.
func WriteProviderSSHConfig(includeFile string, hosts []*ProviderHost, providerName, providerType string, strictHostKeyChecking bool) error {
	paths := config.DefaultPaths()
	content := generateProviderSSHConfig(hosts, providerName, providerType, strictHostKeyChecking)
	destPath := filepath.Join(paths.SSHConfigDir, includeFile)
	if err := atomicWriteFile(destPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write %s: %w", includeFile, err)
	}
	return nil
}

// RemoveProviderSSHConfig removes a provider-owned include file if it exists.
func RemoveProviderSSHConfig(includeFile string) error {
	paths := config.DefaultPaths()
	destPath := filepath.Join(paths.SSHConfigDir, includeFile)
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", includeFile, err)
	}
	return nil
}

func generateProviderSSHConfig(hosts []*ProviderHost, providerName, providerType string, strictHostKeyChecking bool) string {
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Host < hosts[j].Host
	})

	var b strings.Builder
	b.WriteString(providerSSHConfigHeader)
	b.WriteByte('\n')
	fmt.Fprintf(&b, "# Provider: %s (%s)\n", providerName, providerType)

	for _, h := range hosts {
		b.WriteByte('\n')
		if h.Group != "" {
			fmt.Fprintf(&b, "# Group: %s\n", h.Group)
		}
		patterns := h.Patterns
		if len(patterns) == 0 {
			patterns = []string{h.Host}
		}
		fmt.Fprintf(&b, "Host %s\n", strings.Join(patterns, " "))
		fmt.Fprintf(&b, "  HostName %s\n", h.HostName)
		if ignoreHostKeysForProvider(providerType, strictHostKeyChecking) {
			b.WriteString("  StrictHostKeyChecking no\n")
			b.WriteString("  UserKnownHostsFile /dev/null\n")
			b.WriteString("  GlobalKnownHostsFile /dev/null\n")
		}
		if h.Username != "" {
			fmt.Fprintf(&b, "  User %s\n", h.Username)
		}
		if h.Port > 0 && h.Port != 22 {
			fmt.Fprintf(&b, "  Port %d\n", h.Port)
		}
		for _, ct := range h.CompatFixes {
			cfg, ok := compat.CompatConfigs[ct]
			if !ok {
				continue
			}
			for _, line := range cfg.ConfigLines {
				b.WriteString(line)
			}
		}
		if h.ProxyJump != "" {
			fmt.Fprintf(&b, "  ProxyJump %s\n", h.ProxyJump)
		}
		b.WriteString("  PubkeyAuthentication yes\n")
		b.WriteString("  PasswordAuthentication no\n")
	}
	return b.String()
}

func ignoreHostKeysForProvider(providerType string, strictHostKeyChecking bool) bool {
	return providerType == config.ProviderContainerlab && !strictHostKeyChecking
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".inventory-*.tmp")
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
	tmpPath = ""
	return nil
}
