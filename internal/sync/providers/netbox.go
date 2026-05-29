package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/sync"
)

const (
	defaultNetBoxURLEnv   = "NETBOX_URL"
	defaultNetBoxTokenEnv = "NETBOX_TOKEN"
	defaultNetBoxEnvFile  = "~/.env"
)

// NetBoxProvider discovers device inventory from the NetBox API.
type NetBoxProvider struct {
	Client *http.Client
}

// NewNetBoxProvider returns a NetBox provider with a default HTTP client.
func NewNetBoxProvider() *NetBoxProvider {
	return &NetBoxProvider{
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

type netboxListResponse struct {
	Next    string         `json:"next"`
	Results []netboxDevice `json:"results"`
}

type netboxNamedListResponse struct {
	Results []netboxNamedRef `json:"results"`
}

type netboxDevice struct {
	ID         int               `json:"id"`
	Name       string            `json:"name"`
	Status     netboxChoice      `json:"status"`
	Role       *netboxNamedRef   `json:"role"`
	Platform   *netboxNamedRef   `json:"platform"`
	DeviceType *netboxDeviceType `json:"device_type"`
	Site       *netboxNamedRef   `json:"site"`
	Tenant     *netboxNamedRef   `json:"tenant"`
	Tags       []netboxNamedRef  `json:"tags"`
}

type netboxChoice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type netboxNamedRef struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type netboxDeviceType struct {
	Model        string          `json:"model"`
	Slug         string          `json:"slug"`
	Manufacturer *netboxNamedRef `json:"manufacturer"`
}

// Discover fetches all NetBox devices and normalizes them into inventory
// objects for routing and reconciliation.
func (p *NetBoxProvider) Discover(ctx context.Context, source config.SyncSourceConfig, _ sync.RemoteRunner) ([]sync.InventoryObject, error) {
	if source.NetBox == nil {
		return nil, fmt.Errorf("netbox config missing for source %q", source.Name)
	}
	var err error
	tokenEnv := source.NetBox.TokenEnv
	if tokenEnv == "" {
		tokenEnv = defaultNetBoxTokenEnv
	}
	envFile := source.NetBox.EnvFile
	if envFile == "" {
		envFile = defaultNetBoxEnvFile
	}

	baseURL := strings.TrimSpace(source.NetBox.BaseURL)
	if baseURL == "" {
		urlEnv := source.NetBox.URLEnv
		if urlEnv == "" {
			urlEnv = defaultNetBoxURLEnv
		}
		baseURL, err = loadEnvValue(urlEnv, envFile)
		if err != nil {
			return nil, err
		}
	}

	token, err := loadEnvValue(tokenEnv, envFile)
	if err != nil {
		return nil, err
	}

	slog.Debug("netbox discovery start",
		"source", source.Name,
		"base_url", baseURL,
		"url_env", source.NetBox.URLEnv,
		"token_env", tokenEnv,
		"env_file", envFile,
	)

	query := buildNetBoxDeviceQuery(ctx, p.Client, baseURL, token, source.Routes)
	slog.Debug("netbox discovery filters", "source", source.Name, "query", query.Encode())

	devices, err := p.fetchDevices(ctx, baseURL, token, query)
	if err != nil {
		return nil, err
	}

	slog.Debug("netbox discovery complete", "source", source.Name, "devices", len(devices))

	return NormalizeNetBoxDevices(devices, source.Name), nil
}

func (p *NetBoxProvider) fetchDevices(ctx context.Context, baseURL, token string, query url.Values) ([]netboxDevice, error) {
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	nextURL := strings.TrimRight(baseURL, "/") + "/api/dcim/devices/"
	if len(query) == 0 {
		query = make(url.Values)
	}
	query.Set("limit", "100")
	nextURL += "?" + query.Encode()
	var devices []netboxDevice
	seenURLs := make(map[string]struct{})
	pageNum := 0

	for nextURL != "" {
		if _, seen := seenURLs[nextURL]; seen {
			return nil, fmt.Errorf("netbox pagination loop detected at %s", nextURL)
		}
		seenURLs[nextURL] = struct{}{}
		pageNum++
		slog.Debug("netbox fetch page", "page", pageNum, "url", nextURL)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Token "+token)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request %s: %w", nextURL, err)
		}

		var page netboxListResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&page)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			if decodeErr == nil {
				return nil, fmt.Errorf("netbox API %s returned %s", nextURL, resp.Status)
			}
			return nil, fmt.Errorf("netbox API %s returned %s", nextURL, resp.Status)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode %s: %w", nextURL, decodeErr)
		}

		devices = append(devices, page.Results...)
		slog.Debug("netbox page complete",
			"page", pageNum,
			"results", len(page.Results),
			"total", len(devices),
			"has_next", page.Next != "",
		)
		nextURL = page.Next
	}

	return devices, nil
}

// NormalizeNetBoxDevices converts NetBox devices to the common inventory model.
func NormalizeNetBoxDevices(devices []netboxDevice, sourceName string) []sync.InventoryObject {
	objects := make([]sync.InventoryObject, 0, len(devices))
	for i := range devices {
		dev := &devices[i]
		name := strings.TrimSpace(dev.Name)
		if name == "" {
			continue
		}

		fqdn, domainSuffix := deriveNetBoxFQDN(name)
		attrs := map[string][]string{
			"status": appendValues(nil, dev.Status.Value, dev.Status.Label),
		}
		if domainSuffix != "" {
			attrs["domain_suffix"] = []string{domainSuffix}
		}
		if dev.DeviceType != nil {
			attrs["device_type_slug"] = appendValues(nil, dev.DeviceType.Slug)
			if dev.DeviceType.Manufacturer != nil {
				attrs["manufacturer"] = appendNameAndSlug(nil, dev.DeviceType.Manufacturer)
			}
		}
		if dev.Platform != nil {
			attrs["platform"] = appendNameAndSlug(nil, dev.Platform)
		}
		if dev.Role != nil {
			attrs["role"] = appendNameAndSlug(nil, dev.Role)
		}
		if dev.Site != nil {
			attrs["site"] = appendNameAndSlug(nil, dev.Site)
		}
		if dev.Tenant != nil {
			attrs["tenant"] = appendNameAndSlug(nil, dev.Tenant)
		}
		for _, tag := range dev.Tags {
			attrs["tag"] = appendNameAndSlug(attrs["tag"], &tag)
		}

		hostName := name
		if fqdn != "" {
			hostName = fqdn
		}

		objects = append(objects, sync.InventoryObject{
			Provider:        config.ProviderNetBox,
			Source:          sourceName,
			ObjectID:        strconv.Itoa(dev.ID),
			ObjectType:      "device",
			Name:            name,
			FQDN:            fqdn,
			HostName:        hostName,
			UsesPassword:    false,
			CredentialClass: "",
			Attributes:      attrs,
		})
	}

	return objects
}

func deriveNetBoxFQDN(name string) (fqdn, domainSuffix string) {
	name = strings.TrimSpace(name)
	if idx := strings.Index(name, "."); idx != -1 && idx < len(name)-1 {
		return name, name[idx:]
	}
	return "", ""
}

func appendNameAndSlug(dst []string, ref *netboxNamedRef) []string {
	if ref == nil {
		return dst
	}
	return appendValues(dst, ref.Name, ref.Slug)
}

func appendValues(dst []string, values ...string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !containsString(dst, value) {
			dst = append(dst, value)
		}
	}
	return dst
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func buildNetBoxDeviceQuery(ctx context.Context, client *http.Client, baseURL, token string, routes []config.SyncRouteConfig) url.Values {
	if len(routes) == 0 {
		return nil
	}

	query := make(url.Values)
	addResolvedReferenceQueryFilter(ctx, client, baseURL, token, query, routes, "manufacturer", "manufacturer", "/api/dcim/manufacturers/")
	addResolvedReferenceQueryFilter(ctx, client, baseURL, token, query, routes, "tenant", "tenant", "/api/tenancy/tenants/")
	addResolvedReferenceQueryFilter(ctx, client, baseURL, token, query, routes, "role", "role", "/api/dcim/device-roles/")
	addUnionQueryFilter(query, routes, "status", "status")
	addResolvedReferenceQueryFilter(ctx, client, baseURL, token, query, routes, "site", "site", "/api/dcim/sites/")
	addResolvedReferenceQueryFilter(ctx, client, baseURL, token, query, routes, "platform", "platform", "/api/dcim/platforms/")
	addDomainSuffixRegexFilter(query, routes)

	if len(query) == 0 {
		return nil
	}
	return query
}

func addUnionQueryFilter(query url.Values, routes []config.SyncRouteConfig, routeField, queryField string) {
	values, ok := unionRouteMatchValues(routes, routeField)
	if !ok {
		return
	}
	for _, value := range values {
		query.Add(queryField, value)
	}
}

func addResolvedReferenceQueryFilter(ctx context.Context, client *http.Client, baseURL, token string, query url.Values, routes []config.SyncRouteConfig, routeField, queryField, endpoint string) {
	values, ok := unionRouteMatchValues(routes, routeField)
	if !ok {
		return
	}

	resolved := resolveNetBoxReferenceValues(ctx, client, baseURL, token, endpoint, routeField, values)
	for _, value := range resolved {
		query.Add(queryField, value)
	}
}

func addDomainSuffixRegexFilter(query url.Values, routes []config.SyncRouteConfig) {
	suffixes, ok := unionRouteMatchValues(routes, "domain_suffix")
	if !ok || len(suffixes) == 0 {
		return
	}

	patterns := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		suffix = strings.TrimSpace(suffix)
		if suffix == "" {
			continue
		}
		patterns = append(patterns, regexp.QuoteMeta(suffix))
	}
	if len(patterns) == 0 {
		return
	}

	query.Set("name__iregex", "^[A-Za-z0-9._-]+(?:"+strings.Join(patterns, "|")+")$")
}

func unionRouteMatchValues(routes []config.SyncRouteConfig, field string) ([]string, bool) {
	values := make(map[string]struct{})
	for _, route := range routes {
		matches := route.Match[field]
		if len(matches) == 0 {
			return nil, false
		}
		for _, value := range matches {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			values[value] = struct{}{}
		}
	}
	if len(values) == 0 {
		return nil, false
	}

	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out, true
}

func resolveNetBoxReferenceValues(ctx context.Context, client *http.Client, baseURL, token, endpoint, field string, values []string) []string {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	resolved := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		slug, ok := lookupNetBoxReferenceSlug(ctx, client, baseURL, token, endpoint, value)
		if !ok {
			slog.Debug("netbox query filter unresolved", "field", field, "value", value)
			continue
		}
		if _, exists := seen[slug]; exists {
			continue
		}
		seen[slug] = struct{}{}
		resolved = append(resolved, slug)
	}
	sort.Strings(resolved)
	return resolved
}

func lookupNetBoxReferenceSlug(ctx context.Context, client *http.Client, baseURL, token, endpoint, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}

	for _, field := range []string{"slug", "name"} {
		slug, ok := queryNetBoxReferenceSlug(ctx, client, baseURL, token, endpoint, field, value)
		if ok {
			return slug, true
		}
	}

	return "", false
}

func queryNetBoxReferenceSlug(ctx context.Context, client *http.Client, baseURL, token, endpoint, field, value string) (string, bool) {
	query := url.Values{}
	query.Set("limit", "2")
	query.Set(field, value)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Authorization", "Token "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	var result netboxNamedListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false
	}

	for _, item := range result.Results {
		if strings.EqualFold(item.Slug, value) || strings.EqualFold(item.Name, value) {
			return item.Slug, true
		}
	}
	if len(result.Results) == 1 && strings.TrimSpace(result.Results[0].Slug) != "" {
		return result.Results[0].Slug, true
	}

	return "", false
}

func loadEnvValue(name, envFile string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value, nil
	}

	path := expandHomePath(envFile)
	values, err := readDotEnv(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("environment variable %q is not set and env file %q was not found", name, path)
		}
		return "", fmt.Errorf("read env file %q: %w", path, err)
	}

	value := strings.TrimSpace(values[name])
	if value == "" {
		return "", fmt.Errorf("environment variable %q is not set and was not found in %q", name, path)
	}
	return value, nil
}

func readDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key != "" {
			values[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return values, nil
}

func expandHomePath(path string) string {
	path = strings.TrimSpace(path)
	switch {
	case path == "":
		return path
	case path == "~":
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	case strings.HasPrefix(path, "~/"):
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
