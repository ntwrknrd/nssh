package credential

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

type onePasswordProvider struct {
	account   string
	vault     string
	hostRefs  map[string]config.CredentialRefConfig
	groupRefs map[string]config.CredentialRefConfig
	runner    opRunner
	cache     bool
}

type opRunner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

type opCLIRunner struct{}

type onePasswordItem struct {
	Title    string             `json:"title"`
	Category string             `json:"category,omitempty"`
	Fields   []onePasswordField `json:"fields"`
}

type onePasswordField struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	Label   string `json:"label"`
	Value   string `json:"value"`
}

type cachedOnePasswordRecord struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Ref      string `json:"ref"`
}

type cachedOnePasswordItem struct {
	Found bool            `json:"found"`
	Item  onePasswordItem `json:"item,omitempty"`
}

func newOnePasswordProvider(cfg config.CredentialConfig) Provider {
	return &onePasswordProvider{
		account:   cfg.Config.Account,
		vault:     cfg.Config.Vault,
		hostRefs:  cfg.Host,
		groupRefs: cfg.Group,
		runner:    opCLIRunner{},
		cache:     true,
	}
}

func (opCLIRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "op", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("op %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (p *onePasswordProvider) GetHost(host string) (*Record, error) {
	return p.get(scopeHost, host)
}

func (p *onePasswordProvider) HasHostCredential(host string) bool {
	return strings.TrimSpace(p.refForScope(scopeHost, host).Ref) != ""
}

func (p *onePasswordProvider) SetHost(host string, record *Record) error {
	return p.set(scopeHost, host, record)
}

func (p *onePasswordProvider) RemoveHost(host string) (bool, error) {
	return p.remove(scopeHost, host)
}

func (p *onePasswordProvider) GetGroup(group string) (*Record, error) {
	return p.get(scopeGroup, group)
}

func (p *onePasswordProvider) SetGroup(group string, record *Record) error {
	return p.set(scopeGroup, group, record)
}

func (p *onePasswordProvider) RemoveGroup(group string) (bool, error) {
	return p.remove(scopeGroup, group)
}

func (p *onePasswordProvider) Status() Status {
	if p.runner == nil {
		p.runner = opCLIRunner{}
	}
	if _, err := p.runner.Run(context.Background(), nil, "--version"); err != nil {
		return Status{Type: config.CredentialProvider1Password, Available: false, Detail: err.Error()}
	}
	return Status{Type: config.CredentialProvider1Password, Available: true, Detail: "1Password vault " + p.vault}
}

type credentialScope string

const (
	scopeHost  credentialScope = "host"
	scopeGroup credentialScope = "group"
)

func (p *onePasswordProvider) get(scope credentialScope, name string) (*Record, error) {
	if ref := p.refForScope(scope, name); ref.Ref != "" {
		return p.getRef(ref)
	}
	item, err := p.getItem(scope, name)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, nil
	}
	username, password := itemField(item, "username"), itemField(item, "password")
	if username == "" && password == "" {
		return nil, nil
	}
	return &Record{Username: username, Secret: secret.NewFromString(password), Ref: itemTitle(scope, name)}, nil
}

func (p *onePasswordProvider) getRef(ref config.CredentialRefConfig) (*Record, error) {
	if record, ok := p.getCachedRef(ref); ok {
		return record, nil
	}

	if isOnePasswordSecretRef(ref.Ref) {
		username, err := p.resolveUsername(ref)
		if err != nil {
			return nil, err
		}
		password, err := p.readSecretRef(ref.Ref)
		if err != nil {
			return nil, err
		}
		if username == "" && password == "" {
			return nil, nil
		}
		record := &Record{Username: username, Secret: secret.NewFromString(password), Ref: ref.Ref}
		p.putCachedRef(ref, record)
		return record, nil
	}

	item, err := p.getItemByRef(ref.Ref)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, nil
	}
	username := strings.TrimSpace(ref.Username)
	if username == "" && ref.UsernameRef != "" {
		username, err = p.readSecretRef(ref.UsernameRef)
		if err != nil {
			return nil, err
		}
	}
	if username == "" {
		username = itemField(item, "username")
	}
	password := itemField(item, "password")
	if username == "" && password == "" {
		return nil, nil
	}
	record := &Record{Username: username, Secret: secret.NewFromString(password), Ref: ref.Ref}
	p.putCachedRef(ref, record)
	return record, nil
}

func (p *onePasswordProvider) getCachedRef(ref config.CredentialRefConfig) (*Record, bool) {
	client, ok := p.cacheClient()
	if !ok {
		return nil, false
	}
	defer func() { _ = client.Close() }()

	found, data, err := client.CacheGet(p.cacheKey(ref))
	if err != nil || !found {
		return nil, false
	}

	var cached cachedOnePasswordRecord
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}
	if cached.Username == "" && cached.Password == "" {
		return nil, false
	}
	return &Record{
		Username: cached.Username,
		Secret:   secret.NewFromString(cached.Password),
		Ref:      cached.Ref,
	}, true
}

func (p *onePasswordProvider) putCachedRef(ref config.CredentialRefConfig, record *Record) {
	if record == nil || record.Secret == nil {
		return
	}
	client, ok := p.cacheClient()
	if !ok {
		return
	}
	defer func() { _ = client.Close() }()

	cached := cachedOnePasswordRecord{
		Username: record.Username,
		Ref:      record.Ref,
	}
	if err := record.Secret.UseString(func(s string) error {
		cached.Password = s
		return nil
	}); err != nil {
		return
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return
	}
	_ = client.CachePut(p.cacheKey(ref), data)
}

func (p *onePasswordProvider) cacheClient() (*agent.Client, bool) {
	if !p.cache {
		return nil, false
	}
	client, err := agent.Connect()
	if err == nil {
		return client, true
	}
	if !errors.Is(err, agent.ErrAgentNotRunning) {
		return nil, false
	}
	if err := agent.SpawnCache(); err != nil {
		return nil, false
	}
	client, err = agent.Connect()
	if err != nil {
		return nil, false
	}
	return client, true
}

func (p *onePasswordProvider) cacheKey(ref config.CredentialRefConfig) string {
	data, _ := json.Marshal(struct {
		Account     string                     `json:"account"`
		Vault       string                     `json:"vault"`
		Credential  config.CredentialRefConfig `json:"credential"`
		ProviderKey string                     `json:"provider_key"`
	}{
		Account:     p.account,
		Vault:       p.vault,
		Credential:  ref,
		ProviderKey: "1password",
	})
	sum := sha256.Sum256(data)
	return "credential:1password:" + hex.EncodeToString(sum[:])
}

func (p *onePasswordProvider) itemCacheKey(ref string) string {
	data, _ := json.Marshal(struct {
		Account     string `json:"account"`
		Vault       string `json:"vault"`
		Ref         string `json:"ref"`
		ProviderKey string `json:"provider_key"`
	}{
		Account:     p.account,
		Vault:       p.vault,
		Ref:         ref,
		ProviderKey: "1password:item",
	})
	sum := sha256.Sum256(data)
	return "credential:1password:item:" + hex.EncodeToString(sum[:])
}

func (p *onePasswordProvider) set(scope credentialScope, name string, record *Record) error {
	if record == nil || record.Secret == nil {
		return errors.New("1Password credential requires a secret value")
	}
	password := ""
	if err := record.Secret.UseString(func(s string) error {
		password = s
		return nil
	}); err != nil {
		return err
	}
	item := buildItem(itemTitle(scope, name), record.Username, password)
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshal 1Password item: %w", err)
	}

	existing, err := p.getItem(scope, name)
	if err != nil {
		return err
	}
	if existing == nil {
		args := append([]string{"item", "create"}, p.scopeArgs()...)
		args = append(args, "-")
		if _, err = p.run(context.Background(), data, args...); err != nil {
			return err
		}
		p.putCachedItem(item.Title, &item)
		return nil
	}

	args := append([]string{"item", "edit", itemTitle(scope, name)}, p.scopeArgs()...)
	args = append(args, "-")
	if _, err = p.run(context.Background(), data, args...); err != nil {
		return err
	}
	p.putCachedItem(item.Title, &item)
	return nil
}

func (p *onePasswordProvider) remove(scope credentialScope, name string) (bool, error) {
	existing, err := p.getItem(scope, name)
	if err != nil {
		return false, err
	}
	if existing == nil {
		return false, nil
	}
	args := append([]string{"item", "delete", itemTitle(scope, name)}, p.scopeArgs()...)
	_, err = p.run(context.Background(), nil, args...)
	if err == nil {
		p.putCachedItem(itemTitle(scope, name), nil)
	}
	return err == nil, err
}

func (p *onePasswordProvider) getItem(scope credentialScope, name string) (*onePasswordItem, error) {
	return p.getItemByRef(itemTitle(scope, name))
}

func (p *onePasswordProvider) getItemByRef(ref string) (*onePasswordItem, error) {
	if item, ok := p.getCachedItem(ref); ok {
		return item, nil
	}

	args := append([]string{"item", "get", ref}, p.scopeArgs()...)
	args = append(args, "--format", "json", "--reveal")
	out, err := p.run(context.Background(), nil, args...)
	if err != nil {
		if isItemNotFound(out, err) {
			p.putCachedItem(ref, nil)
			return nil, nil
		}
		return nil, err
	}
	var item onePasswordItem
	if err := json.Unmarshal(out, &item); err != nil {
		return nil, fmt.Errorf("parse 1Password item %q: %w", ref, err)
	}
	p.putCachedItem(ref, &item)
	return &item, nil
}

func (p *onePasswordProvider) getCachedItem(ref string) (*onePasswordItem, bool) {
	client, ok := p.cacheClient()
	if !ok {
		return nil, false
	}
	defer func() { _ = client.Close() }()

	found, data, err := client.CacheGet(p.itemCacheKey(ref))
	if err != nil || !found {
		return nil, false
	}

	var cached cachedOnePasswordItem
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}
	if !cached.Found {
		return nil, true
	}
	return &cached.Item, true
}

func (p *onePasswordProvider) putCachedItem(ref string, item *onePasswordItem) {
	client, ok := p.cacheClient()
	if !ok {
		return
	}
	defer func() { _ = client.Close() }()

	cached := cachedOnePasswordItem{Found: item != nil}
	if item != nil {
		cached.Item = *item
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return
	}
	_ = client.CachePut(p.itemCacheKey(ref), data)
}

func (p *onePasswordProvider) readSecretRef(ref string) (string, error) {
	args := []string{"read", ref}
	if p.account != "" {
		args = append(args, "--account", p.account)
	}
	out, err := p.run(context.Background(), nil, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (p *onePasswordProvider) resolveUsername(ref config.CredentialRefConfig) (string, error) {
	if ref.Username != "" {
		return ref.Username, nil
	}
	usernameRef := ref.UsernameRef
	if usernameRef == "" {
		usernameRef = siblingSecretRef(ref.Ref, "username")
	}
	if usernameRef == "" {
		return "", nil
	}
	return p.readSecretRef(usernameRef)
}

func (p *onePasswordProvider) refForScope(scope credentialScope, name string) config.CredentialRefConfig {
	if scope == scopeHost && p.hostRefs != nil {
		return p.hostRefs[name]
	}
	if scope == scopeGroup && p.groupRefs != nil {
		return p.groupRefs[name]
	}
	return config.CredentialRefConfig{}
}

func (p *onePasswordProvider) run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	if p.runner == nil {
		p.runner = opCLIRunner{}
	}
	return p.runner.Run(ctx, stdin, args...)
}

func (p *onePasswordProvider) scopeArgs() []string {
	var args []string
	if p.vault != "" {
		args = append(args, "--vault", p.vault)
	}
	if p.account != "" {
		args = append(args, "--account", p.account)
	}
	return args
}

func itemTitle(scope credentialScope, name string) string {
	return "nssh " + string(scope) + " " + name
}

func buildItem(title, username, password string) onePasswordItem {
	return onePasswordItem{
		Title:    title,
		Category: "LOGIN",
		Fields: []onePasswordField{
			{ID: "username", Type: "STRING", Purpose: "USERNAME", Label: "username", Value: username},
			{ID: "password", Type: "CONCEALED", Purpose: "PASSWORD", Label: "password", Value: password},
		},
	}
}

func itemField(item *onePasswordItem, label string) string {
	for _, field := range item.Fields {
		if strings.EqualFold(field.Label, label) || strings.EqualFold(field.ID, label) {
			return field.Value
		}
	}
	return ""
}

func isItemNotFound(out []byte, err error) bool {
	text := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "isn't an item") ||
		strings.Contains(text, "is not an item") ||
		strings.Contains(text, "does not exist")
}

func isOnePasswordSecretRef(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), "op://")
}

func siblingSecretRef(ref, field string) string {
	ref = strings.TrimSpace(ref)
	idx := strings.LastIndex(ref, "/")
	if idx == -1 || idx == len(ref)-1 {
		return ""
	}
	return ref[:idx+1] + field
}
