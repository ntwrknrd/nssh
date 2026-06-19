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

func RenderSSHOptions(opts config.SSHHostConfig, sshVerbose int) []string {
	args := []string{"-F", "none"}
	if sshVerbose > 3 {
		sshVerbose = 3
	}
	if sshVerbose > 0 {
		args = append(args, "-"+strings.Repeat("v", sshVerbose))
	}
	appendO := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			rendered := value
			if !strings.EqualFold(key, "SetEnv") {
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
