package inventory

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestReconcileRoutesObjectsToGroups(t *testing.T) {
	routes := []config.InventoryRouteConfig{{
		Group: "custcbb",
		Match: config.InventoryRouteMatch{
			"manufacturer": {"Juniper"},
			"status":       {"active"},
		},
	}}
	objects := []Object{{
		ObjectID: "device:1",
		Name:     "edge01",
		HostName: "edge01.custcbb.local",
		Attributes: map[string][]string{
			"manufacturer": {"Juniper"},
			"status":       {"active"},
		},
	}}

	plan := Reconcile(objects, routes, "netbox-prod", nil)
	if len(plan.Adds) != 1 {
		t.Fatalf("adds = %d, want 1", len(plan.Adds))
	}
	if plan.Adds[0].Group != "custcbb" {
		t.Fatalf("group = %q", plan.Adds[0].Group)
	}
}

func TestReconcileDoesNotTrackCredentialClass(t *testing.T) {
	routes := []config.InventoryRouteConfig{{
		Group: "lab",
		Match: config.InventoryRouteMatch{"kind": {"ceos"}},
	}}
	objects := []Object{{
		ObjectID:   "lab/core01",
		Name:       "clab-core01",
		HostName:   "172.20.0.2",
		Attributes: map[string][]string{"kind": {"ceos"}},
	}}

	plan := Reconcile(objects, routes, "nre-netlab01", nil)
	if len(plan.Adds) != 1 {
		t.Fatalf("adds = %d, want 1", len(plan.Adds))
	}
	if plan.Adds[0].ProviderType != "" {
		t.Fatalf("provider type metadata should be assigned by refresh writer, got %q", plan.Adds[0].ProviderType)
	}
}
