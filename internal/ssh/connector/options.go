//go:build unix

package connector

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
)

// SSHOptionPlan describes the precedence sources for an OpenSSH invocation.
// Enforced options are nssh-owned safety policy, Runtime contains the operator's
// explicit arguments, and Resolved contains merged inventory/YAML policy.
type SSHOptionPlan struct {
	Enforced     []string
	Runtime      []string
	Resolved     config.SSHHostConfig
	SSHVerbosity int
}

// ComposeSSHOptions renders SSH options in effective precedence order.
// OpenSSH keeps the first obtained value for most scalar options, so runtime
// arguments must precede resolved configuration. nssh owns the config-file
// boundary and always emits one authoritative -F none.
func ComposeSSHOptions(plan SSHOptionPlan) []string {
	args := slices.Clone(plan.Enforced)
	args = append(args, "-F", "none")
	args = appendSSHVerbosity(args, plan.SSHVerbosity)
	args = append(args, withoutSSHConfigFileOptions(plan.Runtime)...)
	args = append(args, renderResolvedSSHOptions(plan.Resolved)...)
	return args
}

func RenderSSHOptions(opts config.SSHHostConfig, sshVerbose int) []string {
	args := []string{"-F", "none"}
	args = appendSSHVerbosity(args, sshVerbose)
	args = append(args, renderResolvedSSHOptions(opts)...)
	return args
}

func appendSSHVerbosity(args []string, sshVerbose int) []string {
	if sshVerbose > 3 {
		sshVerbose = 3
	}
	if sshVerbose > 0 {
		args = append(args, "-"+strings.Repeat("v", sshVerbose))
	}
	return args
}

func renderResolvedSSHOptions(opts config.SSHHostConfig) []string {
	var args []string
	appendO := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			rendered := value
			if !strings.EqualFold(key, "SetEnv") && !strings.EqualFold(key, "ProxyCommand") {
				rendered = renderOpenSSHOptionValue(value)
			}
			args = append(args, "-o", key+"="+rendered)
		}
	}
	appendOptionValue := func(key string, value config.SSHOptionValue) {
		for _, rendered := range renderSSHOptionValues(key, value) {
			appendO(key, rendered)
		}
	}
	compatibilityOptions := make(map[string]string)
	appendCompatibilityFloor := func(category compat.Category, floor string) {
		if floor == "" {
			return
		}
		policy, ok := compat.AlgorithmPolicies[category]
		if !ok {
			return
		}
		algorithms := compat.AlgorithmsAtOrAboveFloor(category, floor, compat.LocalSupportedAlgorithms(category))
		if len(algorithms) == 0 {
			return
		}
		if key, value, ok := findSSHOption(opts.Options, policy.Directive); ok {
			compatibilityOptions[strings.ToLower(key)] = strings.Join(mergeAlgorithmOption(value, algorithms), ",")
			return
		}
		appendO(policy.Directive, "+"+strings.Join(algorithms, ","))
	}
	appendCompatibilityFloor(compat.CategoryKex, opts.Compatibility.Kex)
	appendCompatibilityFloor(compat.CategoryMAC, opts.Compatibility.MAC)
	appendCompatibilityFloor(compat.CategoryHostKey, opts.Compatibility.HostKey)
	appendCompatibilityFloor(compat.CategoryPublicKey, opts.Compatibility.PublicKey)
	if len(opts.Options) > 0 {
		keys := make([]string, 0, len(opts.Options))
		for key := range opts.Options {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if rendered, ok := compatibilityOptions[strings.ToLower(key)]; ok {
				appendO(key, rendered)
				continue
			}
			appendOptionValue(key, opts.Options[key])
		}
	}
	return args
}

func withoutSSHConfigFileOptions(args []string) []string {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-F" {
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "-F") && len(arg) > 2 {
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

// EffectiveSSHOption returns the first scalar value OpenSSH will obtain from
// args. It recognizes -o forms and the short aliases needed by connection
// orchestration decisions.
func EffectiveSSHOption(args []string, want string) string {
	return effectiveSSHOption(args, want)
}

func effectiveSSHOption(args []string, want string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return ""
		}
		if value, consumed, ok := shortSSHOptionValue(args, i, want); ok {
			return value
		} else if consumed {
			i++
			continue
		}
		if arg == "-o" && i+1 < len(args) {
			key, value, ok := splitOpenSSHOption(args[i+1])
			if ok && strings.EqualFold(key, want) {
				return value
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "-o") && len(arg) > 2 {
			key, value, ok := splitOpenSSHOption(arg[2:])
			if ok && strings.EqualFold(key, want) {
				return value
			}
		}
	}
	return ""
}

func shortSSHOptionValue(args []string, index int, want string) (value string, consumed, ok bool) {
	aliases := map[string]string{
		"bindaddress":   "-b",
		"ciphers":       "-c",
		"controlpath":   "-S",
		"identityfile":  "-i",
		"localforward":  "-L",
		"macs":          "-m",
		"port":          "-p",
		"proxyjump":     "-J",
		"remoteforward": "-R",
		"user":          "-l",
	}
	alias := aliases[strings.ToLower(want)]
	if alias == "" {
		return "", false, false
	}
	arg := args[index]
	if arg == alias {
		if index+1 >= len(args) {
			return "", false, false
		}
		return args[index+1], true, true
	}
	if strings.HasPrefix(arg, alias) && len(arg) > len(alias) {
		return arg[len(alias):], false, true
	}
	return "", false, false
}

func splitOpenSSHOption(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	if key, value, ok := strings.Cut(raw, "="); ok {
		return strings.TrimSpace(key), strings.TrimSpace(value), true
	}
	fields := strings.Fields(raw)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], strings.Join(fields[1:], " "), true
}

func findSSHOption(options config.SSHOptions, key string) (string, config.SSHOptionValue, bool) {
	for candidate, value := range options {
		if strings.EqualFold(candidate, key) {
			return candidate, value, true
		}
	}
	return "", config.SSHOptionValue{}, false
}

func mergeAlgorithmOption(value config.SSHOptionValue, compatibility []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(value.Items)+len(compatibility))
	add := func(algorithm string) {
		algorithm = strings.TrimSpace(algorithm)
		if algorithm == "" || seen[algorithm] {
			return
		}
		seen[algorithm] = true
		out = append(out, algorithm)
	}
	for _, rendered := range renderSSHOptionValues("", value) {
		for _, algorithm := range strings.Split(rendered, ",") {
			add(algorithm)
		}
	}
	for _, algorithm := range compatibility {
		add(algorithm)
	}
	return out
}

func renderSSHOptionValues(key string, value config.SSHOptionValue) []string {
	normalized := strings.ToLower(key)
	switch {
	case normalized == "setenv" && len(value.Map) > 0:
		return []string{value.StringValue()}
	case config.IsRepeatedSSHOption(normalized) && len(value.Items) > 0:
		return slices.Clone(value.Items)
	case config.IsCommaListSSHOption(normalized) && len(value.Items) > 0:
		return []string{strings.Join(value.Items, ",")}
	case config.IsDurationSSHOption(normalized):
		return []string{renderDurationOptionValue(value.StringValue())}
	default:
		return []string{value.StringValue()}
	}
}

func renderDurationOptionValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	if _, err := strconv.Atoi(value); err == nil {
		return value
	}
	d, err := config.Duration(0), error(nil)
	if err = (&d).UnmarshalText([]byte(value)); err != nil {
		return value
	}
	return fmt.Sprintf("%d", int(d.Duration().Seconds()))
}

func renderOpenSSHOptionValue(value string) string {
	if isOpenSSHQuoted(value) || !strings.ContainsAny(value, " \t\r\n\"") {
		return value
	}
	return strconv.Quote(value)
}

func isOpenSSHQuoted(value string) bool {
	return len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"'
}
