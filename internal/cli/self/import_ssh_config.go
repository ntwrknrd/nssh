package self

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/spf13/cobra"
)

type sshImportBlock struct {
	Patterns   []string
	Directives []sshImportDirective
}

type sshImportDirective struct {
	Key   string
	Value string
	Line  int
}

// NewImportCmd creates the import command group.
func NewImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import external configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(NewImportSSHConfigCmd())
	return cmd
}

// NewImportSSHConfigCmd creates the OpenSSH config import command.
func NewImportSSHConfigCmd() *cobra.Command {
	paths := config.DefaultPaths()
	var (
		source   = filepath.Join(homeDir(), ".ssh", "config")
		out      = filepath.Join(paths.ConfigDir, "inventory", "imported-ssh.yaml")
		provider = config.ProviderLocal
		dryRun   bool
	)

	cmd := &cobra.Command{
		Use:   "ssh-config",
		Short: "Import OpenSSH config",
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Open(expandHome(source))
			if err != nil {
				return fmt.Errorf("open ssh config: %w", err)
			}
			defer func() { _ = f.Close() }()

			text, warnings, err := importSSHConfigText(provider, f)
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
			}
			if dryRun {
				fmt.Fprint(cmd.OutOrStdout(), text)
				return nil
			}
			target := expandHome(out)
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return fmt.Errorf("create output dir: %w", err)
			}
			if err := os.WriteFile(target, []byte(text), 0600); err != nil {
				return fmt.Errorf("write %s: %w", target, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", target)
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", source, "OpenSSH config path")
	cmd.Flags().StringVar(&out, "out", out, "output YAML inventory path")
	cmd.Flags().StringVar(&provider, "provider", provider, "inventory provider name")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print YAML instead of writing")
	return cmd
}

func importSSHConfigText(provider string, r io.Reader) (string, []string, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = config.ProviderLocal
	}
	blocks, warnings, err := parseSSHImportBlocks(r)
	if err != nil {
		return "", warnings, err
	}

	providerCfg := config.InventoryProviderConfig{
		Type:   config.ProviderLocal,
		Groups: map[string]config.GroupConfig{"imported": {}},
		Hosts:  make(map[string]config.InventoryHostConfig),
	}
	cfg := &config.Config{
		Inventory: config.InventoryConfig{
			Providers: map[string]config.InventoryProviderConfig{
				provider: providerCfg,
			},
		},
	}

	for _, block := range blocks {
		if len(block.Patterns) == 0 {
			continue
		}
		if isGlobalPattern(block.Patterns) {
			applySSHImportDirectives(&cfg.SSH.Defaults, &cfg.Inventory.Auth, block.Directives, &warnings)
			continue
		}
		hostName := block.Patterns[0]
		if shouldSkipHostPattern(hostName) {
			warnings = append(warnings, fmt.Sprintf("skipping unsupported Host pattern %q", hostName))
			continue
		}
		host := config.InventoryHostConfig{
			Group:    "imported",
			Hostname: hostName,
		}
		for _, alias := range block.Patterns[1:] {
			if shouldSkipHostPattern(alias) {
				warnings = append(warnings, fmt.Sprintf("skipping unsupported Host alias %q for %s", alias, hostName))
				continue
			}
			host.Aliases = append(host.Aliases, alias)
		}
		applySSHImportHostDirectives(&host, block.Directives)
		applySSHImportDirectives(&host.SSH, &host.Auth, block.Directives, &warnings)
		if host.Auth.AuthMode == "" && host.Auth.Username != "" {
			host.Auth.AuthMode = config.AuthModePassword
		}
		providerCfg.Hosts[hostName] = host
	}
	cfg.Inventory.Providers[provider] = providerCfg

	text, err := config.MarshalSparse(cfg)
	if err != nil {
		return "", warnings, err
	}
	return text, warnings, nil
}

func parseSSHImportBlocks(r io.Reader) ([]sshImportBlock, []string, error) {
	var blocks []sshImportBlock
	var warnings []string
	var current *sshImportBlock
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := splitSSHDirective(line)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("line %d ignored: %s", lineNo, line))
			continue
		}
		if strings.EqualFold(key, "Host") {
			block := sshImportBlock{Patterns: strings.Fields(value)}
			blocks = append(blocks, block)
			current = &blocks[len(blocks)-1]
			continue
		}
		if strings.EqualFold(key, "Match") || strings.EqualFold(key, "Include") {
			warnings = append(warnings, fmt.Sprintf("line %d %s directive is not imported", lineNo, key))
			current = nil
			continue
		}
		if current == nil {
			if shouldWarnUnsupportedDirective(key) {
				warnings = append(warnings, fmt.Sprintf("line %d %s directive is not imported", lineNo, key))
			}
			continue
		}
		current.Directives = append(current.Directives, sshImportDirective{Key: key, Value: value, Line: lineNo})
	}
	if err := scanner.Err(); err != nil {
		return nil, warnings, err
	}
	return blocks, warnings, nil
}

func splitSSHDirective(line string) (string, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	key := fields[0]
	value := strings.TrimSpace(line[len(key):])
	if value == "" {
		return "", "", false
	}
	return key, value, true
}

func applySSHImportHostDirectives(host *config.InventoryHostConfig, directives []sshImportDirective) {
	for _, directive := range directives {
		switch strings.ToLower(directive.Key) {
		case "hostname":
			host.Hostname = directive.Value
		case "port":
			if n, err := strconv.Atoi(directive.Value); err == nil {
				host.Port = n
			}
		}
	}
}

func applySSHImportDirectives(sshCfg *config.SSHHostConfig, auth *config.InventoryAuthConfig, directives []sshImportDirective, warnings *[]string) {
	for _, directive := range directives {
		key := directive.Key
		value := directive.Value
		switch strings.ToLower(key) {
		case "hostname":
			// HostName is handled by the caller because it is inventory metadata.
			continue
		case "user":
			auth.Username = value
		case "port":
			// Port is handled by the caller because it is inventory metadata.
			continue
		case "proxyjump":
			sshCfg.ProxyJump = value
		case "proxycommand":
			sshCfg.ProxyCommand = value
		case "identityagent":
			sshCfg.IdentityAgent.Path = value
		case "identityfile":
			sshCfg.IdentityFiles = append(sshCfg.IdentityFiles, value)
		case "certificatefile":
			sshCfg.CertificateFiles = append(sshCfg.CertificateFiles, value)
		case "identitiesonly":
			if b, ok := parseSSHBool(value); ok {
				sshCfg.IdentitiesOnly = &b
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "forwardagent":
			if b, ok := parseSSHBool(value); ok {
				sshCfg.ForwardAgent = &b
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "localforward":
			sshCfg.LocalForwards = append(sshCfg.LocalForwards, parseSSHForward(value))
		case "remoteforward":
			sshCfg.RemoteForwards = append(sshCfg.RemoteForwards, parseSSHForward(value))
		case "setenv":
			if sshCfg.SetEnv == nil {
				sshCfg.SetEnv = make(map[string]string)
			}
			for _, item := range strings.Fields(value) {
				k, v, ok := strings.Cut(item, "=")
				if ok && k != "" {
					sshCfg.SetEnv[k] = v
				}
			}
		case "remotecommand":
			sshCfg.RemoteCommand = value
		case "serveraliveinterval":
			if d, ok := parseSSHDuration(value); ok {
				sshCfg.ServerAliveInterval = config.Duration(d)
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "serveralivecountmax":
			if n, err := strconv.Atoi(value); err == nil {
				sshCfg.ServerAliveCountMax = n
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "connecttimeout":
			if d, ok := parseSSHDuration(value); ok {
				sshCfg.ConnectionTimeout = config.Duration(d)
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "controlmaster":
			sshCfg.ControlMaster = value
		case "controlpersist":
			if d, ok := parseSSHDuration(value); ok {
				sshCfg.ControlPersist = config.Duration(d)
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "controlpath":
			sshCfg.ControlPath = value
		case "ciphers":
			sshCfg.Ciphers = splitCSV(value)
		case "macs":
			sshCfg.MACs = splitCSV(value)
		case "kexalgorithms":
			sshCfg.KexAlgorithms = splitCSV(value)
		case "hostkeyalgorithms":
			sshCfg.HostKeyAlgorithms = splitCSV(value)
		case "pubkeyacceptedalgorithms":
			sshCfg.PubkeyAcceptedAlgorithms = splitCSV(value)
		case "canonicalizehostname":
			*warnings = append(*warnings, fmt.Sprintf("line %d %s directive is not imported", directive.Line, key))
		default:
			addSSHImportOption(sshCfg, key, value)
		}
	}
}

func parseSSHBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "on":
		return true, true
	case "no", "false", "off":
		return false, true
	default:
		return false, false
	}
}

func parseSSHDuration(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second, true
	}
	d, err := time.ParseDuration(value)
	return d, err == nil
}

func parseSSHForward(value string) config.Forward {
	fields := strings.Fields(value)
	if len(fields) >= 2 {
		return config.Forward{Bind: fields[0], Target: strings.Join(fields[1:], " ")}
	}
	return config.Forward{Bind: value}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func addSSHImportOption(sshCfg *config.SSHHostConfig, key, value string) {
	if sshCfg.Options == nil {
		sshCfg.Options = make(map[string]string)
	}
	sshCfg.Options[key] = value
}

func isGlobalPattern(patterns []string) bool {
	return len(patterns) == 1 && patterns[0] == "*"
}

func shouldSkipHostPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?!")
}

func shouldWarnUnsupportedDirective(key string) bool {
	switch strings.ToLower(key) {
	case "canonicalizehostname":
		return true
	default:
		return false
	}
}

func expandHome(path string) string {
	if path == "~" {
		return homeDir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), strings.TrimPrefix(path, "~/"))
	}
	return path
}
