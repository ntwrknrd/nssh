package sync

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestMatchRouteFirstMatchWins(t *testing.T) {
	routes := []config.SyncRouteConfig{
		{Name: "route-a", Context: "lab", Match: config.SyncRouteMatch{"credential_class": {"ceos"}}},
		{Name: "route-b", Context: "prod", Match: config.SyncRouteMatch{"credential_class": {"ceos"}}},
	}

	obj := &InventoryObject{CredentialClass: "ceos"}
	r := MatchRoute(obj, routes)
	if r == nil {
		t.Fatal("expected a match")
	} else if r.Name != "route-a" {
		t.Errorf("expected route-a, got %q", r.Name)
	}
}

func TestMatchRouteANDAcrossFields(t *testing.T) {
	routes := []config.SyncRouteConfig{{
		Name:    "strict",
		Context: "lab",
		Match: config.SyncRouteMatch{
			"credential_class": {"ceos"},
			"name":             {"clab-core01"},
		},
	}}

	// Both fields match
	obj := &InventoryObject{Name: "clab-core01", CredentialClass: "ceos"}
	if r := MatchRoute(obj, routes); r == nil {
		t.Error("expected match when both fields match")
	}

	// Only one field matches
	obj2 := &InventoryObject{Name: "clab-core01", CredentialClass: "vjunos"}
	if r := MatchRoute(obj2, routes); r != nil {
		t.Error("expected no match when one field differs")
	}
}

func TestMatchRouteORWithinField(t *testing.T) {
	routes := []config.SyncRouteConfig{{
		Name:    "multi-kind",
		Context: "lab",
		Match:   config.SyncRouteMatch{"credential_class": {"ceos", "vjunos"}},
	}}

	for _, class := range []string{"ceos", "vjunos"} {
		obj := &InventoryObject{CredentialClass: class}
		if r := MatchRoute(obj, routes); r == nil {
			t.Errorf("expected match for class %q", class)
		}
	}

	obj := &InventoryObject{CredentialClass: "nokia-srl"}
	if r := MatchRoute(obj, routes); r != nil {
		t.Error("expected no match for nokia-srl")
	}
}

func TestMatchRouteAttributes(t *testing.T) {
	routes := []config.SyncRouteConfig{{
		Name:    "running-only",
		Context: "lab",
		Match:   config.SyncRouteMatch{"state": {"running"}},
	}}

	obj := &InventoryObject{
		Attributes: map[string][]string{"state": {"running"}},
	}
	if r := MatchRoute(obj, routes); r == nil {
		t.Error("expected match on attribute")
	}

	obj2 := &InventoryObject{
		Attributes: map[string][]string{"state": {"stopped"}},
	}
	if r := MatchRoute(obj2, routes); r != nil {
		t.Error("expected no match for stopped")
	}
}

func TestMatchRouteNoRoutes(t *testing.T) {
	obj := &InventoryObject{Name: "anything"}
	if r := MatchRoute(obj, nil); r != nil {
		t.Error("expected nil for empty routes")
	}
}

func TestMatchRouteEmptyMatch(t *testing.T) {
	// A route with empty match should match everything
	routes := []config.SyncRouteConfig{{
		Name:    "catch-all",
		Context: "default",
		Match:   config.SyncRouteMatch{},
	}}

	obj := &InventoryObject{Name: "anything", CredentialClass: "whatever"}
	r := MatchRoute(obj, routes)
	if r == nil {
		t.Error("empty match should match everything")
	}
}

func TestMatchRouteNoAttributes(t *testing.T) {
	routes := []config.SyncRouteConfig{{
		Name:    "needs-attr",
		Context: "lab",
		Match:   config.SyncRouteMatch{"custom_field": {"val"}},
	}}

	// Object with no attributes should not match
	obj := &InventoryObject{Name: "no-attrs"}
	if r := MatchRoute(obj, routes); r != nil {
		t.Error("expected no match when object has no attributes")
	}
}

func TestResolveDestination(t *testing.T) {
	tests := []struct {
		name            string
		route           config.SyncRouteConfig
		source          string
		wantContext     string
		wantIncludeFile string
	}{
		{
			name:            "explicit include_file",
			route:           config.SyncRouteConfig{Context: "lab", IncludeFile: "conf.d/clab_nre-netlab01"},
			source:          "nre-netlab01",
			wantContext:     "lab",
			wantIncludeFile: "conf.d/clab_nre-netlab01",
		},
		{
			name:            "default include_file",
			route:           config.SyncRouteConfig{Context: "prod"},
			source:          "netbox-prod",
			wantContext:     "prod",
			wantIncludeFile: "conf.d/sync_netbox-prod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, incl := ResolveDestination(&tt.route, tt.source)
			if ctx != tt.wantContext {
				t.Errorf("context = %q, want %q", ctx, tt.wantContext)
			}
			if incl != tt.wantIncludeFile {
				t.Errorf("include_file = %q, want %q", incl, tt.wantIncludeFile)
			}
		})
	}
}
