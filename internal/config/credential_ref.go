package config

import "strings"

// DefaultCredentialRef returns the conventional provider ref for an inventory
// host or provider-qualified group target.
func DefaultCredentialRef(provider, target string, group bool) string {
	if group {
		switch {
		case provider == "sops":
			target = strings.ReplaceAll(target, "/", ".")
			return "groups." + target + ".password"
		case strings.HasPrefix(provider, "op-"), strings.HasPrefix(provider, "bw-"):
			return "nssh group " + target
		default:
			target = strings.ReplaceAll(target, "/", ".")
			return "groups." + target + ".password"
		}
	}
	switch {
	case provider == "sops":
		target = strings.ReplaceAll(target, "/", ".")
		return "hosts." + target + ".password"
	case strings.HasPrefix(provider, "op-"), strings.HasPrefix(provider, "bw-"):
		return "nssh host " + target
	default:
		target = strings.ReplaceAll(target, "/", ".")
		return "hosts." + target + ".password"
	}
}
