package config

import "strings"

// DefaultCredentialRef returns the conventional provider ref for an inventory
// host or provider-qualified group target.
func DefaultCredentialRef(provider, target string, group bool) string {
	if group {
		switch {
		case strings.HasPrefix(provider, "op-"), strings.HasPrefix(provider, "bw-"):
			return "nssh group " + target
		default:
			return "nssh/groups/" + target
		}
	}
	switch {
	case strings.HasPrefix(provider, "op-"), strings.HasPrefix(provider, "bw-"):
		return "nssh host " + target
	default:
		return "nssh/hosts/" + target
	}
}
