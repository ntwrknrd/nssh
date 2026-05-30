package inv

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

type hostPatch struct {
	Host     string
	Group    string
	HostName string
	User     string
	Port     int
	PortSet  bool
}

type hostMetadata struct {
	Owner string
	Group string
}

type importResult struct {
	Added   int
	Skipped int
	Failed  int
	Errors  []string
}

func upsertLocalHost(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, patch hostPatch) error {
	if patch.Host == "" {
		return fmt.Errorf("host is required")
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return err
	}
	groupName := patch.Group
	if groupName == "" {
		groupName = cfg.Inventory.DefaultGroup
	}
	if _, ok := cfg.Inventory.Group[groupName]; !ok {
		return fmt.Errorf("group %q not found", groupName)
	}
	targetFile := localFilePath(paths, inventory.LocalProviderIncludeFile())

	existing, existingCfg, err := findInventoryHostWithLocation(parser, cfg, paths, patch.Host)
	if err != nil {
		return err
	}
	if existing != nil && metadataForHost(existing, cfg, paths, nil).Owner != "local" {
		return fmt.Errorf("host %q is provider-owned; change provider route config instead", patch.Host)
	}

	var host *sshconfig.HostEntry
	if existing != nil {
		host = cloneHostEntry(existing)
		applyHostPatch(host, patch)
	} else {
		port := patch.Port
		if !patch.PortSet {
			port = 22
		}
		host = sshconfig.CreateHostEntry(patch.Host, patch.HostName, patch.User, port, false, targetFile)
	}
	host.SourceFile = targetFile
	inventory.SetLocalHostGroup(host, groupName)

	if existingCfg != nil && existingCfg.Path != targetFile {
		existingCfg.Hosts = sshconfig.RemoveHost(existingCfg.Hosts, patch.Host)
		sshconfig.SortHosts(existingCfg.Hosts)
		if err := writeParsedConfig(parser, existingCfg, paths); err != nil {
			return err
		}
	}

	targetCfg, err := parser.ParseFile(targetFile)
	if err != nil {
		return err
	}
	targetCfg.Hosts = sshconfig.RemoveHost(targetCfg.Hosts, patch.Host)
	idx := sshconfig.FindInsertionIndex(targetCfg.Hosts, host.Host)
	targetCfg.Hosts = append(targetCfg.Hosts[:idx], append([]*sshconfig.HostEntry{host}, targetCfg.Hosts[idx:]...)...)
	sshconfig.SortHosts(targetCfg.Hosts)
	return writeParsedConfig(parser, targetCfg, paths)
}

func removeLocalHost(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, hostName string) (bool, error) {
	if hostName == "" {
		return false, fmt.Errorf("host is required")
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return false, err
	}
	host, parsed, err := findInventoryHostWithLocation(parser, cfg, paths, hostName)
	if err != nil {
		return false, err
	}
	if host == nil || parsed == nil {
		return false, nil
	}
	if metadataForHost(host, cfg, paths, nil).Owner != "local" {
		return false, fmt.Errorf("host %q is provider-owned; change provider route config instead", hostName)
	}
	parsed.Hosts = sshconfig.RemoveHost(parsed.Hosts, hostName)
	sshconfig.SortHosts(parsed.Hosts)
	return true, writeParsedConfig(parser, parsed, paths)
}

func importLocalCSV(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, csvPath, group string) (*importResult, error) {
	if csvPath == "" {
		return nil, fmt.Errorf("CSV file is required")
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return nil, err
	}
	file, err := os.Open(csvPath)
	if err != nil {
		return nil, fmt.Errorf("open CSV: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	columns := make(map[string]int, len(header))
	for i, col := range header {
		columns[strings.ToLower(strings.TrimSpace(col))] = i
	}
	if _, ok := columns["host"]; !ok {
		return nil, fmt.Errorf("CSV file missing required host column")
	}

	result := &importResult{}
	line := 1
	for {
		line++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, fmt.Errorf("line %d: %w", line, err)
		}
		patch := hostPatch{
			Host:     csvValue(record, columns, "host"),
			HostName: csvValue(record, columns, "hostname"),
			User:     csvValue(record, columns, "user"),
		}
		if patch.Host == "" {
			result.Skipped++
			continue
		}
		patch.Group = group
		if patch.Group == "" {
			patch.Group = csvValue(record, columns, "group")
		}
		if portValue := csvValue(record, columns, "port"); portValue != "" {
			port, err := strconv.Atoi(portValue)
			if err != nil || port < 1 {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid port %q", line, portValue))
				continue
			}
			patch.Port = port
			patch.PortSet = true
		}
		if err := upsertLocalHost(parser, cfg, paths, patch); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("line %d host %s: %v", line, patch.Host, err))
			continue
		}
		result.Added++
	}
	return result, nil
}

func csvValue(record []string, columns map[string]int, name string) string {
	idx, ok := columns[name]
	if !ok || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}

func localFilePath(paths *config.Paths, localFile string) string {
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if filepath.IsAbs(localFile) {
		return localFile
	}
	if strings.ContainsRune(localFile, filepath.Separator) {
		return filepath.Join(paths.SSHConfigDir, localFile)
	}
	return filepath.Join(paths.SSHConfigDir, "nssh.d", localFile)
}

func inventoryHosts(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths) ([]*sshconfig.HostEntry, error) {
	if parser == nil {
		parser = sshconfig.NewParser()
	}
	files, err := inventoryFiles(cfg, paths)
	if err != nil {
		return nil, err
	}
	hosts := make([]*sshconfig.HostEntry, 0)
	for _, file := range files {
		parsed, err := parser.ParseFile(file)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, parsed.Hosts...)
	}
	return hosts, nil
}

func findInventoryHostWithLocation(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, pattern string) (*sshconfig.HostEntry, *sshconfig.ParsedConfig, error) {
	if parser == nil {
		parser = sshconfig.NewParser()
	}
	files, err := inventoryFiles(cfg, paths)
	if err != nil {
		return nil, nil, err
	}
	for _, file := range files {
		parsed, err := parser.ParseFile(file)
		if err != nil {
			return nil, nil, err
		}
		if host := sshconfig.FindHostByPattern(parsed.Hosts, pattern); host != nil {
			return host, parsed, nil
		}
	}
	return nil, nil, nil
}

func inventoryFiles(cfg *config.Config, paths *config.Paths) ([]string, error) {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	files := make([]string, 0, 1+len(cfg.Inventory.Provider))
	add := func(path string) {
		clean := filepath.Clean(path)
		if seen[clean] {
			return
		}
		seen[clean] = true
		files = append(files, clean)
	}
	add(localFilePath(paths, inventory.LocalProviderIncludeFile()))
	for name := range cfg.Inventory.Provider {
		add(localFilePath(paths, inventory.ProviderIncludeFile(name)))
	}
	sort.Strings(files)
	return files, nil
}

func metadataForHost(host *sshconfig.HostEntry, cfg *config.Config, paths *config.Paths, index map[string]*inventory.HostInfo) hostMetadata {
	if host == nil {
		return hostMetadata{Owner: "local"}
	}
	if index != nil {
		if info := index[host.Host]; info != nil {
			return hostMetadata{Owner: info.Provider, Group: info.Group}
		}
		for _, pattern := range host.Patterns {
			if info := index[pattern]; info != nil {
				return hostMetadata{Owner: info.Provider, Group: info.Group}
			}
		}
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	for name := range cfg.Inventory.Provider {
		if samePath(host.SourceFile, localFilePath(paths, inventory.ProviderIncludeFile(name))) {
			return hostMetadata{Owner: name, Group: "-"}
		}
	}
	group := cfg.Inventory.DefaultGroup
	if group == "" {
		group = "default"
	}
	if samePath(host.SourceFile, localFilePath(paths, inventory.LocalProviderIncludeFile())) {
		return hostMetadata{Owner: inventory.LocalProviderName, Group: inventory.LocalHostGroup(host, group)}
	}
	return hostMetadata{Owner: "local", Group: group}
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return filepath.Clean(absA) == filepath.Clean(absB)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func applyHostPatch(host *sshconfig.HostEntry, patch hostPatch) {
	if patch.HostName != "" {
		upsertDirective(host, "HostName", patch.HostName)
		host.HostName = patch.HostName
		host.Properties["hostname"] = patch.HostName
	}
	if patch.User != "" {
		upsertDirective(host, "User", patch.User)
		host.Properties["user"] = patch.User
	}
	if patch.PortSet {
		upsertDirective(host, "Port", fmt.Sprintf("%d", patch.Port))
		host.Properties["port"] = fmt.Sprintf("%d", patch.Port)
	}
}

func upsertDirective(host *sshconfig.HostEntry, key, value string) {
	line := fmt.Sprintf("  %s %s\n", key, value)
	re := regexp.MustCompile(`(?i)^\s*` + regexp.QuoteMeta(key) + `\s+`)
	for i, existing := range host.Lines {
		if re.MatchString(existing) {
			host.Lines[i] = line
			return
		}
	}
	insertAt := 1
	if len(host.Lines) > 1 {
		insertAt = len(host.Lines)
		if strings.TrimSpace(host.Lines[len(host.Lines)-1]) == "" {
			insertAt = len(host.Lines) - 1
		}
	}
	host.Lines = append(host.Lines[:insertAt], append([]string{line}, host.Lines[insertAt:]...)...)
}

func cloneHostEntry(host *sshconfig.HostEntry) *sshconfig.HostEntry {
	clone := *host
	clone.Patterns = append([]string(nil), host.Patterns...)
	clone.Lines = append([]string(nil), host.Lines...)
	clone.Properties = make(map[string]string, len(host.Properties))
	for key, value := range host.Properties {
		clone.Properties[key] = value
	}
	return &clone
}

func writeParsedConfig(parser *sshconfig.Parser, parsed *sshconfig.ParsedConfig, paths *config.Paths) error {
	if err := backupFile(parsed.Path, paths.BackupDir); err != nil {
		return err
	}
	return parser.WriteFile(parsed)
}

func backupFile(srcPath, backupDir string) error {
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return nil
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open backup source: %w", err)
	}
	defer func() { _ = src.Close() }()

	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%s.bak", filepath.Base(srcPath), time.Now().Format("20060102_150405")))
	dst, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("create backup: %w", err)
	}
	defer func() { _ = dst.Close() }()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return nil
}
