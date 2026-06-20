package self

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
)

func credentialProviderReadiness(provider config.CredentialProviderConfig) (bool, string) {
	command := credentialProviderCommand(provider)
	label := credentialProviderStatusLabel(provider.Type)
	if command == "" {
		return false, label + ": unsupported provider type"
	}
	if path, err := exec.LookPath(command); err == nil {
		if provider.Type == config.CredentialProviderPass {
			return passProviderReadiness(provider, path)
		}
		return true, fmt.Sprintf("%s%s: ready (%s)", label, providerSessionSuffix(provider), AbbreviatePath(path))
	}
	return false, fmt.Sprintf("%s%s: missing %s", label, providerSessionSuffix(provider), command)
}

func passProviderReadiness(provider config.CredentialProviderConfig, passPath string) (bool, string) {
	label := "Pass" + providerSessionSuffix(provider)
	if _, err := exec.LookPath("gpg"); err != nil {
		return false, fmt.Sprintf("%s: missing gpg", label)
	}
	if _, err := exec.LookPath("gpgconf"); err != nil {
		return false, fmt.Sprintf("%s: missing gpgconf", label)
	}
	if out, err := exec.Command("gpg", "--list-secret-keys", "--with-colons").Output(); err != nil || !hasGPGSecretKey(string(out)) {
		return false, fmt.Sprintf("%s: no GPG secret key", label)
	}
	storeDir := passStoreDir()
	if info, err := os.Stat(storeDir); err != nil || !info.IsDir() {
		return false, fmt.Sprintf("%s: password store missing (%s)", label, AbbreviatePath(storeDir))
	}
	gpgID := filepath.Join(storeDir, ".gpg-id")
	if info, err := os.Stat(gpgID); err != nil || info.IsDir() {
		return false, fmt.Sprintf("%s: password store not initialized (.gpg-id missing)", label)
	}
	return true, fmt.Sprintf("%s: ready (%s)", label, AbbreviatePath(passPath))
}

func providerSessionSuffix(provider config.CredentialProviderConfig) string {
	session := provider.Config.Session
	if session == "" {
		session = provider.Session
	}
	if session == "" {
		return ""
	}
	return " " + session
}

func hasGPGSecretKey(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "sec:") {
			return true
		}
	}
	return false
}

func passStoreDir() string {
	if dir := strings.TrimSpace(os.Getenv("PASSWORD_STORE_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".password-store"
	}
	return filepath.Join(home, ".password-store")
}

func credentialProviderCommand(provider config.CredentialProviderConfig) string {
	if provider.Config.Command != "" {
		return provider.Config.Command
	}
	if provider.Command != "" {
		return provider.Command
	}
	switch provider.Type {
	case config.CredentialProviderPass:
		return "pass"
	case config.CredentialProvider1Password:
		return "op"
	case config.CredentialProviderBitwarden:
		return "bw"
	default:
		return ""
	}
}

func credentialProviderStatusLabel(providerType string) string {
	switch providerType {
	case config.CredentialProviderPass:
		return "Pass"
	case config.CredentialProvider1Password:
		return "1Password"
	case config.CredentialProviderBitwarden:
		return "Bitwarden"
	default:
		return providerType
	}
}
