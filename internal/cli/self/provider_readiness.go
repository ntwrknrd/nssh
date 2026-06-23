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
		if provider.Type == config.CredentialProviderSOPSAge {
			return sopsAgeProviderReadiness(provider, path)
		}
		return true, fmt.Sprintf("%s: ready (%s)", label, AbbreviatePath(path))
	}
	return false, fmt.Sprintf("%s: missing %s", label, command)
}

func sopsAgeProviderReadiness(provider config.CredentialProviderConfig, sopsPath string) (bool, string) {
	label := "SOPS+age"
	file := strings.TrimSpace(provider.File)
	if file == "" {
		file = strings.TrimSpace(provider.Config.File)
	}
	if file == "" {
		return false, fmt.Sprintf("%s: missing file config", label)
	}
	if info, err := os.Stat(expandSelfPath(file)); err != nil || info.IsDir() {
		return false, fmt.Sprintf("%s: file missing (%s)", label, AbbreviatePath(file))
	}
	ageKeyFile := strings.TrimSpace(provider.AgeKeyFile)
	if ageKeyFile == "" {
		ageKeyFile = strings.TrimSpace(provider.Config.AgeKeyFile)
	}
	if ageKeyFile != "" {
		if info, err := os.Stat(expandSelfPath(ageKeyFile)); err != nil || info.IsDir() {
			return false, fmt.Sprintf("%s: age key file missing (%s)", label, AbbreviatePath(ageKeyFile))
		}
	}
	return true, fmt.Sprintf("%s: ready (%s)", label, AbbreviatePath(sopsPath))
}

func credentialProviderCommand(provider config.CredentialProviderConfig) string {
	if provider.Config.Command != "" {
		return provider.Config.Command
	}
	if provider.Command != "" {
		return provider.Command
	}
	switch provider.Type {
	case config.CredentialProviderSOPSAge:
		return "sops"
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
	case config.CredentialProviderSOPSAge:
		return "SOPS+age"
	case config.CredentialProvider1Password:
		return "1Password"
	case config.CredentialProviderBitwarden:
		return "Bitwarden"
	default:
		return providerType
	}
}

func expandSelfPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		return homeDir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), strings.TrimPrefix(path, "~/"))
	}
	return path
}
