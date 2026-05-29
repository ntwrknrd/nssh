package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestNormalizeNetBoxDevices(t *testing.T) {
	devices := []netboxDevice{
		{
			ID:   42,
			Name: "acm-core1.custcbb.local",
			Status: netboxChoice{
				Value: "active",
				Label: "Active",
			},
			Role:     &netboxNamedRef{Name: "Router", Slug: "router"},
			Platform: &netboxNamedRef{Name: "junos", Slug: "junos"},
			DeviceType: &netboxDeviceType{
				Slug:         "mx480",
				Manufacturer: &netboxNamedRef{Name: "Juniper", Slug: "juniper"},
			},
			Site:   &netboxNamedRef{Name: "ACM", Slug: "acm"},
			Tenant: &netboxNamedRef{Name: "CustCBB", Slug: "custcbb"},
			Tags: []netboxNamedRef{
				{Name: "Core Routing", Slug: "core-routing"},
				{Name: "datapulse-metrics", Slug: "datapulse-metrics"},
			},
		},
		{
			ID:   99,
			Name: "3SG-PLUS-vPAN01",
			Status: netboxChoice{
				Value: "active",
				Label: "Active",
			},
			DeviceType: &netboxDeviceType{
				Slug:         "vm-series",
				Manufacturer: &netboxNamedRef{Name: "Palo Alto", Slug: "palo-alto"},
			},
		},
	}

	objects := NormalizeNetBoxDevices(devices, "netbox-prod")
	if len(objects) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(objects))
	}

	obj := objects[0]
	if obj.Provider != "netbox-prod" {
		t.Fatalf("provider = %q", obj.Provider)
	}
	if obj.ObjectID != "42" {
		t.Fatalf("object_id = %q", obj.ObjectID)
	}
	if obj.ObjectType != "device" {
		t.Fatalf("object_type = %q", obj.ObjectType)
	}
	if obj.Name != "acm-core1.custcbb.local" {
		t.Fatalf("name = %q", obj.Name)
	}
	if obj.FQDN != "acm-core1.custcbb.local" {
		t.Fatalf("fqdn = %q", obj.FQDN)
	}
	if obj.HostName != "acm-core1.custcbb.local" {
		t.Fatalf("hostname = %q", obj.HostName)
	}
	if !slices.Contains(obj.Attributes["manufacturer"], "Juniper") {
		t.Fatalf("manufacturer attrs = %v", obj.Attributes["manufacturer"])
	}
	if !slices.Contains(obj.Attributes["platform"], "junos") {
		t.Fatalf("platform attrs = %v", obj.Attributes["platform"])
	}
	if !slices.Contains(obj.Attributes["device_type_slug"], "mx480") {
		t.Fatalf("device_type_slug attrs = %v", obj.Attributes["device_type_slug"])
	}
	if !slices.Contains(obj.Attributes["domain_suffix"], ".custcbb.local") {
		t.Fatalf("domain_suffix attrs = %v", obj.Attributes["domain_suffix"])
	}
	if !slices.Contains(obj.Attributes["tag"], "Core Routing") || !slices.Contains(obj.Attributes["tag"], "core-routing") {
		t.Fatalf("tag attrs = %v", obj.Attributes["tag"])
	}
	if !slices.Contains(obj.Attributes["status"], "active") {
		t.Fatalf("status attrs = %v", obj.Attributes["status"])
	}

	short := objects[1]
	if short.FQDN != "" {
		t.Fatalf("short fqdn = %q, want empty", short.FQDN)
	}
	if short.HostName != "3SG-PLUS-vPAN01" {
		t.Fatalf("short hostname = %q", short.HostName)
	}
}

func TestNetBoxProviderDiscover(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/dcim/manufacturers/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("name") == "Arista" || r.URL.Query().Get("slug") == "Arista" || r.URL.Query().Get("slug") == "arista":
			_ = json.NewEncoder(w).Encode(netboxNamedListResponse{Results: []netboxNamedRef{{Name: "Arista", Slug: "arista"}}})
		case r.URL.Query().Get("name") == "Juniper" || r.URL.Query().Get("slug") == "Juniper" || r.URL.Query().Get("slug") == "juniper":
			_ = json.NewEncoder(w).Encode(netboxNamedListResponse{Results: []netboxNamedRef{{Name: "Juniper", Slug: "juniper"}}})
		default:
			_ = json.NewEncoder(w).Encode(netboxNamedListResponse{})
		}
	})
	handler.HandleFunc("/api/tenancy/tenants/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("name") == "Expedient" || r.URL.Query().Get("slug") == "Expedient" || r.URL.Query().Get("slug") == "expedient-48":
			_ = json.NewEncoder(w).Encode(netboxNamedListResponse{Results: []netboxNamedRef{{Name: "Expedient", Slug: "expedient-48"}}})
		default:
			_ = json.NewEncoder(w).Encode(netboxNamedListResponse{})
		}
	})
	handler.HandleFunc("/api/dcim/devices/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token test-token" {
			t.Fatalf("authorization header = %q", got)
		}
		if r.URL.Query().Get("limit") != "100" {
			t.Fatalf("limit query = %q", r.URL.Query().Get("limit"))
		}
		if got := r.URL.Query()["manufacturer"]; !slices.Equal(got, []string{"arista", "juniper"}) {
			t.Fatalf("manufacturer query = %v", got)
		}
		if got := r.URL.Query()["tenant"]; !slices.Equal(got, []string{"expedient-48"}) {
			t.Fatalf("tenant query = %v", got)
		}
		if got := r.URL.Query().Get("name__iregex"); got != "^[A-Za-z0-9._-]+(?:\\.custcbb\\.local|\\.expedient\\.com)$" {
			t.Fatalf("name__iregex query = %q", got)
		}
		resp := netboxListResponse{
			Results: []netboxDevice{
				{ID: 1, Name: "edge01.expedient.com", Status: netboxChoice{Value: "active"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	t.Setenv("NB_TEST_TOKEN", "test-token")

	provider := &NetBoxProvider{Client: server.Client()}
	providerCfg := config.InventoryProviderConfig{
		Type: config.ProviderNetBox,
		Config: config.InventoryProviderDetailConfig{
			BaseURL:  server.URL,
			TokenEnv: "NB_TEST_TOKEN",
		},
		Route: []config.InventoryRouteConfig{
			{
				Group: "custcbb",
				Match: config.InventoryRouteMatch{
					"manufacturer":  {"Juniper", "Arista"},
					"tenant":        {"Expedient"},
					"domain_suffix": {".custcbb.local"},
				},
			},
			{
				Group: "cbb",
				Match: config.InventoryRouteMatch{
					"manufacturer":  {"Juniper", "Arista"},
					"tenant":        {"Expedient"},
					"domain_suffix": {".expedient.com"},
				},
			},
		},
	}

	objects, err := provider.Discover(context.Background(), "netbox-prod", providerCfg, nil)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("objects = %d", len(objects))
	}
	if objects[0].HostName != "edge01.expedient.com" {
		t.Fatalf("hostname = %q", objects[0].HostName)
	}
}

func TestNetBoxProviderDiscoverMissingToken(t *testing.T) {
	_ = os.Unsetenv("NB_MISSING")

	provider := NewNetBoxProvider()
	providerCfg := config.InventoryProviderConfig{
		Type: config.ProviderNetBox,
		Config: config.InventoryProviderDetailConfig{
			BaseURL:  "https://netbox.example.com",
			TokenEnv: "NB_MISSING",
		},
	}

	_, err := provider.Discover(context.Background(), "netbox-prod", providerCfg, nil)
	if err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestNetBoxProviderDiscoverLoadsTokenFromEnvFile(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/dcim/devices/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token file-token" {
			t.Fatalf("authorization header = %q", got)
		}
		resp := netboxListResponse{
			Results: []netboxDevice{
				{ID: 1, Name: "edge01.expedient.com", Status: netboxChoice{Value: "active"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envFile, []byte("NB_FILE_URL="+server.URL+"\nNB_FILE_TOKEN=file-token\n"), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	provider := &NetBoxProvider{Client: server.Client()}
	providerCfg := config.InventoryProviderConfig{
		Type: config.ProviderNetBox,
		Config: config.InventoryProviderDetailConfig{
			URLEnv:   "NB_FILE_URL",
			TokenEnv: "NB_FILE_TOKEN",
			EnvFile:  envFile,
		},
	}

	objects, err := provider.Discover(context.Background(), "netbox-prod", providerCfg, nil)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("objects = %d", len(objects))
	}
}

func TestNetBoxProviderDiscoverUsesDefaultEnvFileAndTokenName(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/dcim/devices/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token default-token" {
			t.Fatalf("authorization header = %q", got)
		}
		resp := netboxListResponse{
			Results: []netboxDevice{
				{ID: 1, Name: "edge01.expedient.com", Status: netboxChoice{Value: "active"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("NETBOX_URL", "")
	t.Setenv("NETBOX_TOKEN", "")
	if err := os.WriteFile(filepath.Join(tmpHome, ".env"), []byte("NETBOX_URL="+server.URL+"\nNETBOX_TOKEN=default-token\n"), 0600); err != nil {
		t.Fatalf("write default env file: %v", err)
	}

	provider := &NetBoxProvider{Client: server.Client()}
	providerCfg := config.InventoryProviderConfig{
		Type:   config.ProviderNetBox,
		Config: config.InventoryProviderDetailConfig{},
	}

	objects, err := provider.Discover(context.Background(), "netbox-prod", providerCfg, nil)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("objects = %d", len(objects))
	}
}

func TestNetBoxProviderDetectsPaginationLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := netboxListResponse{
			Next: serverURLWithPath(r.Host, r.URL.Path, r.URL.RawQuery),
			Results: []netboxDevice{
				{ID: 1, Name: "edge01.expedient.com", Status: netboxChoice{Value: "active"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := &NetBoxProvider{Client: server.Client()}
	_, err := provider.fetchDevices(context.Background(), server.URL, "test-token", nil)
	if err == nil {
		t.Fatal("expected pagination loop error")
	}
	if !strings.Contains(err.Error(), "pagination loop detected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildNetBoxDeviceQuery(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/dcim/manufacturers/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("name") == "Arista" || r.URL.Query().Get("slug") == "Arista" || r.URL.Query().Get("slug") == "arista":
			_ = json.NewEncoder(w).Encode(netboxNamedListResponse{Results: []netboxNamedRef{{Name: "Arista", Slug: "arista"}}})
		case r.URL.Query().Get("name") == "Juniper" || r.URL.Query().Get("slug") == "Juniper" || r.URL.Query().Get("slug") == "juniper":
			_ = json.NewEncoder(w).Encode(netboxNamedListResponse{Results: []netboxNamedRef{{Name: "Juniper", Slug: "juniper"}}})
		default:
			_ = json.NewEncoder(w).Encode(netboxNamedListResponse{})
		}
	})
	handler.HandleFunc("/api/tenancy/tenants/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("name") == "Expedient" || r.URL.Query().Get("slug") == "Expedient" || r.URL.Query().Get("slug") == "expedient-48":
			_ = json.NewEncoder(w).Encode(netboxNamedListResponse{Results: []netboxNamedRef{{Name: "Expedient", Slug: "expedient-48"}}})
		default:
			_ = json.NewEncoder(w).Encode(netboxNamedListResponse{})
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	query := buildNetBoxDeviceQuery(context.Background(), server.Client(), server.URL, "test-token", []config.InventoryRouteConfig{
		{
			Group: "custcbb",
			Match: config.InventoryRouteMatch{
				"manufacturer":  {"Juniper", "Arista"},
				"tenant":        {"Expedient"},
				"domain_suffix": {".custcbb.local"},
			},
		},
		{
			Group: "cbb",
			Match: config.InventoryRouteMatch{
				"manufacturer":  {"Arista", "Juniper"},
				"tenant":        {"Expedient"},
				"domain_suffix": {".expedient.com"},
			},
		},
	})

	if got := query["manufacturer"]; !slices.Equal(got, []string{"arista", "juniper"}) {
		t.Fatalf("manufacturer query = %v", got)
	}
	if got := query["tenant"]; !slices.Equal(got, []string{"expedient-48"}) {
		t.Fatalf("tenant query = %v", got)
	}
	if got := query.Get("name__iregex"); got != "^[A-Za-z0-9._-]+(?:\\.custcbb\\.local|\\.expedient\\.com)$" {
		t.Fatalf("name__iregex query = %q", got)
	}
}

func TestBuildNetBoxDeviceQuerySkipsPartialRouteFilters(t *testing.T) {
	query := buildNetBoxDeviceQuery(context.Background(), nil, "https://netbox.example.com", "test-token", []config.InventoryRouteConfig{
		{
			Group: "custcbb",
			Match: config.InventoryRouteMatch{
				"manufacturer": {"Juniper"},
			},
		},
		{
			Group: "cbb",
			Match: config.InventoryRouteMatch{},
		},
	})

	if query == nil {
		return
	}
	if _, ok := query["manufacturer"]; ok {
		t.Fatalf("manufacturer query should be omitted when not present on every route: %v", query["manufacturer"])
	}
}

func TestFetchDevicesIncludesQueryParameters(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/dcim/devices/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("role"); got != "router" {
			t.Fatalf("role query = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "100" {
			t.Fatalf("limit query = %q", got)
		}
		resp := netboxListResponse{
			Results: []netboxDevice{
				{ID: 1, Name: "edge01.expedient.com", Status: netboxChoice{Value: "active"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	provider := &NetBoxProvider{Client: server.Client()}
	query := url.Values{}
	query.Set("role", "router")
	devices, err := provider.fetchDevices(context.Background(), server.URL, "test-token", query)
	if err != nil {
		t.Fatalf("fetchDevices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %d", len(devices))
	}
}

func serverURLWithPath(host, path, rawQuery string) string {
	url := "http://" + host + path
	if rawQuery != "" {
		url += "?" + rawQuery
	}
	return url
}
