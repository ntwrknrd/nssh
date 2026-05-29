package self

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type legacyGroupFileMove struct {
	Group       string
	Source      string
	Destination string
}

type legacyGroupFilePrompt func(legacyGroupFileMove) (bool, error)

// NewUpgradeCmd creates the data upgrade command used for breaking releases.
func NewUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "upgrade",
		Short:  "Upgrade nssh data",
		Long:   "Upgrade released nssh config, inventory metadata, and credential data to the current schema.",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade()
		},
	}
}

func runUpgrade() error {
	continueMigration := os.Getenv("NSSH_UPGRADE_CONTINUE") == "1"
	if !continueMigration && version != "dev" {
		if err := runReinstallRelease(false); err != nil {
			return err
		}
		bin := FindBinary()
		if bin == "" {
			bin = os.Args[0]
		}
		cmd := exec.Command(bin, "self", "upgrade")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(), "NSSH_UPGRADE_CONTINUE=1")
		return cmd.Run()
	}
	return runDataUpgrade()
}

func runDataUpgrade() error {
	ui.CommandStart("UPGRADE NSSH DATA")
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("self upgrade requires an interactive terminal")
	}

	paths := config.DefaultPaths()
	cfg, legacySyncConfig, err := loadConfigForUpgrade(paths.ConfigFile)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	mgr, err := clisession.NewManager(vault.Auto(), vault.WithAppConfig(cfg))
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if err := clisession.EnsureUnlocked(mgr); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	contexts, err := mgr.ListContexts()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	changedConfig, err := migrateConfigToInventory(cfg, contexts, paths, confirmLegacyGroupFileMove)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if err := cfg.Credential.Validate(); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if err := cfg.Inventory.Validate(); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	backupDir := filepath.Join(paths.BackupDir, "upgrade-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("create upgrade backup dir: %w", err)
	}
	if err := backupIfExists(paths.ConfigFile, filepath.Join(backupDir, "config.toml")); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if err := backupIfExists(paths.CredentialsFile, filepath.Join(backupDir, "credentials.age")); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	if changedConfig || legacySyncConfig {
		if err := config.Save(paths.ConfigFile, cfg); err != nil {
			ui.CommandEnd(ui.StatusError)
			return err
		}
	}
	if err := migrateContextCredentials(mgr, contexts); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	ui.Success("Backups: %s", AbbreviatePath(backupDir))
	ui.Success("Upgrade complete")
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func loadConfigForUpgrade(path string) (*config.Config, bool, error) {
	cfg := config.DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, false, nil
		}
		return nil, false, fmt.Errorf("read config: %w", err)
	}
	md, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, false, fmt.Errorf("parse config: %w", err)
	}
	legacySyncConfig := md.IsDefined("sync")
	if !md.IsDefined("inventory", "default_group") {
		cfg.Inventory.DefaultGroup = ""
	}
	if !md.IsDefined("inventory", "group") {
		cfg.Inventory.Group = nil
	}
	if !md.IsDefined("inventory", "provider") {
		cfg.Inventory.Provider = nil
	}
	return cfg, legacySyncConfig, nil
}

func migrateConfigToInventory(
	cfg *config.Config,
	contexts []vault.ContextEntry,
	paths *config.Paths,
	prompt legacyGroupFilePrompt,
) (bool, error) {
	changed := false
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if cfg.Credential.Type == "" {
		cfg.Credential.Type = config.CredentialProviderAge
		changed = true
	}
	if cfg.Inventory.DefaultGroup == "" {
		cfg.Inventory.DefaultGroup = cfg.Host.Defaults.DefaultContext
		if cfg.Inventory.DefaultGroup == "" {
			cfg.Inventory.DefaultGroup = "default"
		}
		changed = true
	}
	if cfg.Inventory.Group == nil {
		cfg.Inventory.Group = make(map[string]config.GroupConfig)
		changed = true
	}
	for _, ctx := range contexts {
		if ctx.Name == "" {
			continue
		}
		group, exists := cfg.Inventory.Group[ctx.Name]
		if !exists {
			changed = true
		}
		if group.LocalFile == "" {
			localFile := oldContextLocalFile(ctx.GitIncludeFile, ctx.Name)
			if localFile != "" && legacyLocalFileExists(paths, localFile) {
				group.LocalFile = localFile
				changed = true
			}
		}
		if len(group.DomainSuffix) == 0 && ctx.Domain != "" {
			group.DomainSuffix = []string{normalizeDomainSuffix(ctx.Domain)}
			changed = true
		}
		cfg.Inventory.Group[ctx.Name] = group
	}
	if len(cfg.Sync.Sources) > 0 {
		cfg.Sync.Sources = nil
		changed = true
	}
	moved, err := migrateLegacyGroupFiles(cfg, paths, prompt)
	if err != nil {
		return false, err
	}
	return changed || moved, nil
}

func oldContextLocalFile(includeFile, group string) string {
	includeFile = strings.TrimSpace(includeFile)
	if includeFile == "" {
		return ""
	}
	if filepath.IsAbs(includeFile) {
		return includeFile
	}
	if strings.ContainsAny(includeFile, `/\`) {
		return includeFile
	}
	return filepath.Join("conf.d", includeFile)
}

func legacyLocalFileExists(paths *config.Paths, localFile string) bool {
	_, err := os.Stat(legacyGroupLocalFilePath(paths, localFile))
	return err == nil
}

func normalizeDomainSuffix(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" || strings.HasPrefix(domain, ".") {
		return domain
	}
	return "." + domain
}

func migrateLegacyGroupFiles(cfg *config.Config, paths *config.Paths, prompt legacyGroupFilePrompt) (bool, error) {
	if cfg.Inventory.Group == nil {
		return false, nil
	}
	changed := false
	for name, group := range cfg.Inventory.Group {
		localFile := strings.TrimSpace(group.LocalFile)
		if localFile == "" || isNsshLocalFilename(localFile) {
			continue
		}

		dstName := "local_" + name + ".conf"
		src := legacyGroupLocalFilePath(paths, localFile)
		dst := filepath.Join(paths.SSHConfigDir, "nssh.d", dstName)
		move := legacyGroupFileMove{
			Group:       name,
			Source:      src,
			Destination: dst,
		}

		if _, err := os.Stat(src); err != nil {
			if !os.IsNotExist(err) {
				return false, fmt.Errorf("stat legacy local inventory file %s: %w", src, err)
			}
			group.LocalFile = dstName
			cfg.Inventory.Group[name] = group
			changed = true
			continue
		}
		if _, err := os.Stat(dst); err == nil {
			return false, fmt.Errorf("cannot move local inventory for group %q: destination already exists: %s", name, dst)
		} else if !os.IsNotExist(err) {
			return false, fmt.Errorf("stat new local inventory file %s: %w", dst, err)
		}

		if prompt != nil {
			ok, err := prompt(move)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, fmt.Errorf("upgrade requires moving local inventory files into ~/.ssh/nssh.d")
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return false, fmt.Errorf("create nssh.d: %w", err)
		}
		if err := os.Rename(src, dst); err != nil {
			return false, fmt.Errorf("move local inventory for group %q: %w", name, err)
		}
		group.LocalFile = dstName
		cfg.Inventory.Group[name] = group
		changed = true
	}
	return changed, nil
}

func isNsshLocalFilename(localFile string) bool {
	return !strings.ContainsAny(localFile, `/\`)
}

func legacyGroupLocalFilePath(paths *config.Paths, localFile string) string {
	if filepath.IsAbs(localFile) {
		return localFile
	}
	if strings.ContainsAny(localFile, `/\`) {
		return filepath.Join(paths.SSHConfigDir, localFile)
	}
	return filepath.Join(paths.SSHConfigDir, "nssh.d", localFile)
}

func confirmLegacyGroupFileMove(move legacyGroupFileMove) (bool, error) {
	ui.Info("Move local inventory file for group %s:", move.Group)
	ui.Info("  %s -> %s", AbbreviatePath(move.Source), AbbreviatePath(move.Destination))
	return ui.Confirm("Move into ~/.ssh/nssh.d?", true)
}

func migrateContextCredentials(mgr *vault.Manager, contexts []vault.ContextEntry) error {
	for _, ctx := range contexts {
		if ctx.Credential == nil {
			continue
		}
		sec := secret.NewFromString(ctx.Credential.Password)
		if err := mgr.SetGroupCredential(ctx.Name, ctx.Credential.Username, sec); err != nil {
			return fmt.Errorf("migrate credential for group %q: %w", ctx.Name, err)
		}
	}
	return nil
}

func backupIfExists(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open backup source %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create backup %s: %w", dst, err)
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy backup %s: %w", dst, err)
	}
	return nil
}
