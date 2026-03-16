package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/sync"
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
	token := strings.TrimSpace(os.Getenv(source.NetBox.TokenEnv))
	if token == "" {
		return nil, fmt.Errorf("environment variable %q is not set", source.NetBox.TokenEnv)
	}

	devices, err := p.fetchDevices(ctx, source.NetBox.BaseURL, token)
	if err != nil {
		return nil, err
	}

	return NormalizeNetBoxDevices(devices, source.Name), nil
}

func (p *NetBoxProvider) fetchDevices(ctx context.Context, baseURL, token string) ([]netboxDevice, error) {
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	nextURL := strings.TrimRight(baseURL, "/") + "/api/dcim/devices/?limit=100"
	var devices []netboxDevice

	for nextURL != "" {
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
