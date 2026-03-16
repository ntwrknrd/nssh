package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
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
	if obj.Provider != config.ProviderNetBox {
		t.Fatalf("provider = %q", obj.Provider)
	}
	if obj.Source != "netbox-prod" {
		t.Fatalf("source = %q", obj.Source)
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
	handler.HandleFunc("/api/dcim/devices/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token test-token" {
			t.Fatalf("authorization header = %q", got)
		}
		if r.URL.Query().Get("limit") != "100" {
			t.Fatalf("limit query = %q", r.URL.Query().Get("limit"))
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
	source := config.SyncSourceConfig{
		Name:     "netbox-prod",
		Provider: config.ProviderNetBox,
		NetBox: &config.NetBoxConfig{
			BaseURL:  server.URL,
			TokenEnv: "NB_TEST_TOKEN",
		},
	}

	objects, err := provider.Discover(context.Background(), source, nil)
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
	source := config.SyncSourceConfig{
		Name:     "netbox-prod",
		Provider: config.ProviderNetBox,
		NetBox: &config.NetBoxConfig{
			BaseURL:  "https://netbox.example.com",
			TokenEnv: "NB_MISSING",
		},
	}

	_, err := provider.Discover(context.Background(), source, nil)
	if err == nil {
		t.Fatal("expected missing token error")
	}
}
