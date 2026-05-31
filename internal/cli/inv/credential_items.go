package inv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
)

type credentialItem struct {
	Label string
	Ref   string
}

var listCredentialItems = listCredentialItemsFromProvider

var runCredentialProviderCommand = func(ctx context.Context, command string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", command, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func listCredentialItemsFromProvider(cfg *config.Config, providerName string) ([]credentialItem, error) {
	if cfg == nil {
		return nil, nil
	}
	providerCfg, ok := cfg.Credential.Provider[providerName]
	if !ok {
		return nil, fmt.Errorf("credential provider %q is not configured", providerName)
	}
	switch providerCfg.Type {
	case config.CredentialProvider1Password:
		return listOnePasswordCredentialItems(providerCfg)
	case config.CredentialProviderPass:
		return listPassCredentialItems(providerCfg)
	default:
		return nil, nil
	}
}

type onePasswordListItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
}

func listOnePasswordCredentialItems(providerCfg config.CredentialProviderConfig) ([]credentialItem, error) {
	args := []string{"item", "list"}
	if providerCfg.Config.Vault != "" {
		args = append(args, "--vault", providerCfg.Config.Vault)
	}
	if providerCfg.Config.Account != "" {
		args = append(args, "--account", providerCfg.Config.Account)
	}
	args = append(args, "--format", "json")
	out, err := runCredentialProviderCommand(context.Background(), "op", nil, args...)
	if err != nil {
		return nil, err
	}
	var raw []onePasswordListItem
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse 1Password item list: %w", err)
	}
	items := make([]credentialItem, 0, len(raw))
	vault := strings.Trim(strings.TrimSpace(providerCfg.Config.Vault), "/")
	for _, item := range raw {
		label := strings.TrimSpace(item.Title)
		ref := strings.TrimSpace(item.ID)
		if ref == "" {
			ref = label
		}
		if ref != "" && vault != "" {
			ref = "op://" + vault + "/" + ref + "/"
		}
		if label == "" || ref == "" {
			continue
		}
		if item.Category != "" {
			label = fmt.Sprintf("%s (%s)", label, item.Category)
		}
		items = append(items, credentialItem{Label: label, Ref: ref})
	}
	sortCredentialItems(items)
	return items, nil
}

func listPassCredentialItems(providerCfg config.CredentialProviderConfig) ([]credentialItem, error) {
	command := strings.TrimSpace(providerCfg.Config.Command)
	if command == "" {
		command = "pass"
	}
	prefix := strings.Trim(strings.TrimSpace(providerCfg.Config.Prefix), "/")
	if prefix == "" {
		prefix = "nssh"
	}
	out, err := runCredentialProviderCommand(context.Background(), command, nil, "ls", prefix)
	if err != nil {
		return nil, err
	}
	items := parsePassCredentialItems(prefix, string(out))
	sortCredentialItems(items)
	return items, nil
}

func parsePassCredentialItems(prefix, output string) []credentialItem {
	prefix = strings.Trim(prefix, "/")
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	parsed := make([]struct {
		level int
		name  string
	}, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		level, name := parsePassTreeLine(line)
		if name == "" {
			continue
		}
		if level < 0 {
			level = 0
		}
		parsed = append(parsed, struct {
			level int
			name  string
		}{level: level, name: name})
	}
	stack := make([]string, 0, 8)
	items := make([]credentialItem, 0)
	for i, line := range parsed {
		level := line.level
		name := line.name
		if level < len(stack) {
			stack = stack[:level]
		}
		for len(stack) < level {
			stack = append(stack, "")
		}
		stack = append(stack, name)
		ref := strings.Join(nonEmptyStrings(stack), "/")
		if ref == "" || ref == prefix {
			continue
		}
		if i+1 < len(parsed) && parsed[i+1].level > level {
			continue
		}
		if prefix != "" && !strings.HasPrefix(ref, prefix+"/") {
			ref = prefix + "/" + ref
		}
		items = append(items, credentialItem{Label: ref, Ref: ref})
	}
	return items
}

func parsePassTreeLine(line string) (int, string) {
	normalized := strings.ReplaceAll(line, "\u2502", " ")
	leading := len(normalized) - len(strings.TrimLeft(normalized, " "))
	level := leading / 4
	name := strings.TrimSpace(normalized)
	name = strings.TrimPrefix(name, "\u251c\u2500\u2500 ")
	name = strings.TrimPrefix(name, "\u2514\u2500\u2500 ")
	return level, strings.TrimSpace(name)
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func sortCredentialItems(items []credentialItem) {
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
}
