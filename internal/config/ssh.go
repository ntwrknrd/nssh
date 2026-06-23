package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type SSHHostConfig struct {
	Compatibility SSHCompatibility `yaml:"compatibility,omitempty"`
	Options       SSHOptions       `yaml:"options,omitempty"`
}

type SSHCompatibility struct {
	Kex       string `yaml:"kex,omitempty"`
	MAC       string `yaml:"mac,omitempty"`
	HostKey   string `yaml:"host_key,omitempty"`
	PublicKey string `yaml:"public_key,omitempty"`
}

type SSHOptions map[string]SSHOptionValue

type SSHOptionValue struct {
	Scalar string
	Bool   *bool
	Items  []string
	Map    map[string]string
}

type sshOptionKind uint8

const (
	sshOptionScalar sshOptionKind = 1 << iota
	sshOptionBool
	sshOptionItems
	sshOptionMap
)

type sshOptionPolicy struct {
	allowed sshOptionKind
	label   string
}

var knownSSHOptionPolicies = map[string]sshOptionPolicy{
	"batchmode":                       {allowed: sshOptionBool, label: "boolean"},
	"challengeresponseauthentication": {allowed: sshOptionBool, label: "boolean"},
	"compression":                     {allowed: sshOptionBool, label: "boolean"},
	"forwardagent":                    {allowed: sshOptionBool, label: "boolean"},
	"gssapiauthentication":            {allowed: sshOptionBool, label: "boolean"},
	"identitiesonly":                  {allowed: sshOptionBool, label: "boolean"},
	"kbdinteractiveauthentication":    {allowed: sshOptionBool, label: "boolean"},
	"passwordauthentication":          {allowed: sshOptionBool, label: "boolean"},
	"pubkeyauthentication":            {allowed: sshOptionBool, label: "boolean"},
	"tcpkeepalive":                    {allowed: sshOptionBool, label: "boolean"},

	"controlmaster":        {allowed: sshOptionScalar, label: "string"},
	"controlpath":          {allowed: sshOptionScalar, label: "string"},
	"globalknownhostsfile": {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},
	"hostname":             {allowed: sshOptionScalar, label: "string"},
	"identityagent":        {allowed: sshOptionScalar, label: "string"},
	"loglevel":             {allowed: sshOptionScalar, label: "string"},
	"proxycommand":         {allowed: sshOptionScalar, label: "string"},
	"proxyjump":            {allowed: sshOptionScalar, label: "string"},
	"remotecommand":        {allowed: sshOptionScalar, label: "string"},
	"userknownhostsfile":   {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},
	"warnweakcrypto":       {allowed: sshOptionScalar, label: "string"},

	"connecttimeout":      {allowed: sshOptionScalar, label: "string"},
	"controlpersist":      {allowed: sshOptionScalar, label: "string"},
	"serveralivecountmax": {allowed: sshOptionScalar, label: "string"},
	"serveraliveinterval": {allowed: sshOptionScalar, label: "string"},

	"certificatefile": {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},
	"identityfile":    {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},
	"localforward":    {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},
	"remoteforward":   {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},

	"ciphers":                  {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},
	"hostkeyalgorithms":        {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},
	"kexalgorithms":            {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},
	"macs":                     {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},
	"preferredauthentications": {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},
	"pubkeyacceptedalgorithms": {allowed: sshOptionScalar | sshOptionItems, label: "string or list"},

	"setenv": {allowed: sshOptionMap, label: "map"},
}

var unknownSSHOptionPolicy = sshOptionPolicy{
	allowed: sshOptionScalar | sshOptionBool | sshOptionItems,
	label:   "scalar, boolean, or list",
}

func NewSSHOptionString(value string) SSHOptionValue {
	return SSHOptionValue{Scalar: value}
}

func NewSSHOptionBool(value bool) SSHOptionValue {
	return SSHOptionValue{Bool: &value}
}

func NewSSHOptionItems(values ...string) SSHOptionValue {
	return SSHOptionValue{Items: slices.Clone(values)}
}

func NewSSHOptionMap(values map[string]string) SSHOptionValue {
	return SSHOptionValue{Map: cloneStringMap(values)}
}

func (v *SSHOptionValue) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag == "!!bool" {
			parsed, err := strconv.ParseBool(node.Value)
			if err != nil {
				return fmt.Errorf("invalid boolean option value %q: %w", node.Value, err)
			}
			v.Bool = &parsed
			v.Scalar = ""
			v.Items = nil
			v.Map = nil
			return nil
		}
		v.Scalar = node.Value
		v.Bool = nil
		v.Items = nil
		v.Map = nil
		return nil
	case yaml.SequenceNode:
		items := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			items = append(items, item.Value)
		}
		v.Items = items
		v.Scalar = ""
		v.Bool = nil
		v.Map = nil
		return nil
	case yaml.MappingNode:
		values := make(map[string]string, len(node.Content)/2)
		for idx := 0; idx+1 < len(node.Content); idx += 2 {
			values[node.Content[idx].Value] = node.Content[idx+1].Value
		}
		v.Map = values
		v.Scalar = ""
		v.Bool = nil
		v.Items = nil
		return nil
	default:
		return fmt.Errorf("unsupported SSH option value kind %d", node.Kind)
	}
}

func (v SSHOptionValue) MarshalYAML() (any, error) {
	if v.Bool != nil {
		return *v.Bool, nil
	}
	if len(v.Items) > 0 {
		return v.Items, nil
	}
	if len(v.Map) > 0 {
		return v.Map, nil
	}
	return v.Scalar, nil
}

func (v SSHOptionValue) IsZero() bool {
	return v.Bool == nil && v.Scalar == "" && len(v.Items) == 0 && len(v.Map) == 0
}

func (v SSHOptionValue) StringValue() string {
	if v.Bool != nil {
		if *v.Bool {
			return "yes"
		}
		return "no"
	}
	if len(v.Items) > 0 {
		return strings.Join(v.Items, ",")
	}
	if len(v.Map) > 0 {
		keys := make([]string, 0, len(v.Map))
		for key := range v.Map {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		values := make([]string, 0, len(keys))
		for _, key := range keys {
			values = append(values, key+"="+v.Map[key])
		}
		return strings.Join(values, " ")
	}
	return v.Scalar
}

func (v SSHOptionValue) kind() sshOptionKind {
	switch {
	case v.Bool != nil:
		return sshOptionBool
	case len(v.Items) > 0:
		return sshOptionItems
	case len(v.Map) > 0:
		return sshOptionMap
	default:
		return sshOptionScalar
	}
}

func validateSSHHostConfig(scope string, cfg SSHHostConfig) error {
	if err := validateSSHOptions(scope+".options", cfg.Options); err != nil {
		return err
	}
	return nil
}

func validateSSHOptions(scope string, options SSHOptions) error {
	for key, value := range options {
		policy, ok := knownSSHOptionPolicies[strings.ToLower(key)]
		if !ok {
			policy = unknownSSHOptionPolicy
		}
		if value.kind()&policy.allowed == 0 {
			return fmt.Errorf("%s.%s must be a %s", scope, key, policy.label)
		}
	}
	return nil
}

// MergeSSH applies nssh SSH inheritance. Values in override replace base when
// they are explicitly set; additive fields such as identity files and forwards
// are appended.
func MergeSSH(base, override SSHHostConfig) SSHHostConfig {
	out := cloneSSH(base)

	out.Compatibility = mergeCompatibility(out.Compatibility, override.Compatibility)
	out.Options = mergeSSHOptions(out.Options, override.Options)

	return out
}

func cloneSSH(in SSHHostConfig) SSHHostConfig {
	out := in
	out.Compatibility = in.Compatibility
	out.Options = cloneSSHOptions(in.Options)
	return out
}

func cloneBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mergeStringMap(base, override map[string]string) map[string]string {
	out := cloneStringMap(base)
	if len(override) == 0 {
		return out
	}
	if out == nil {
		out = make(map[string]string, len(override))
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}

func cloneSSHOptions(in SSHOptions) SSHOptions {
	if len(in) == 0 {
		return nil
	}
	out := make(SSHOptions, len(in))
	for key, value := range in {
		out[key] = SSHOptionValue{
			Scalar: value.Scalar,
			Bool:   cloneBoolPtr(value.Bool),
			Items:  slices.Clone(value.Items),
			Map:    cloneStringMap(value.Map),
		}
	}
	return out
}

func mergeSSHOptions(base, override SSHOptions) SSHOptions {
	out := cloneSSHOptions(base)
	if len(override) == 0 {
		return out
	}
	if out == nil {
		out = make(SSHOptions, len(override))
	}
	for key, value := range override {
		out[key] = mergeSSHOptionValue(key, out[key], value)
	}
	return out
}

func mergeSSHOptionValue(key string, base, override SSHOptionValue) SSHOptionValue {
	normalized := strings.ToLower(key)
	if normalized == "setenv" && len(override.Map) > 0 {
		out := SSHOptionValue{Map: cloneStringMap(base.Map)}
		if out.Map == nil {
			out.Map = make(map[string]string, len(override.Map))
		}
		for name, value := range override.Map {
			out.Map[name] = value
		}
		return out
	}
	if IsRepeatedSSHOption(normalized) && len(override.Items) > 0 {
		out := SSHOptionValue{Items: slices.Clone(base.Items)}
		out.Items = append(out.Items, override.Items...)
		return out
	}
	return SSHOptionValue{
		Scalar: override.Scalar,
		Bool:   cloneBoolPtr(override.Bool),
		Items:  slices.Clone(override.Items),
		Map:    cloneStringMap(override.Map),
	}
}

func IsRepeatedSSHOption(key string) bool {
	key = strings.ToLower(key)
	policy, ok := knownSSHOptionPolicies[key]
	return ok && policy.allowed == (sshOptionScalar|sshOptionItems) &&
		(key == "identityfile" || key == "certificatefile" || key == "localforward" || key == "remoteforward")
}

func IsCommaListSSHOption(key string) bool {
	switch strings.ToLower(key) {
	case "ciphers", "macs", "kexalgorithms", "hostkeyalgorithms", "pubkeyacceptedalgorithms", "preferredauthentications":
		return true
	default:
		return false
	}
}

func IsDurationSSHOption(key string) bool {
	switch strings.ToLower(key) {
	case "serveraliveinterval", "connecttimeout", "controlpersist":
		return true
	default:
		return false
	}
}

func mergeCompatibility(base, override SSHCompatibility) SSHCompatibility {
	out := base
	if override.Kex != "" {
		out.Kex = override.Kex
	}
	if override.MAC != "" {
		out.MAC = override.MAC
	}
	if override.HostKey != "" {
		out.HostKey = override.HostKey
	}
	if override.PublicKey != "" {
		out.PublicKey = override.PublicKey
	}
	return out
}
