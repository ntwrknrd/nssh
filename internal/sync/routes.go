package sync

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
)

// normalizedFields are the top-level InventoryObject fields that can be
// matched in route rules without falling through to Attributes.
var normalizedFields = map[string]func(*InventoryObject) string{
	"provider":         func(o *InventoryObject) string { return o.Provider },
	"source":           func(o *InventoryObject) string { return o.Source },
	"object_type":      func(o *InventoryObject) string { return o.ObjectType },
	"name":             func(o *InventoryObject) string { return o.Name },
	"fqdn":             func(o *InventoryObject) string { return o.FQDN },
	"hostname":         func(o *InventoryObject) string { return o.HostName },
	"credential_class": func(o *InventoryObject) string { return o.CredentialClass },
}

// MatchRoute evaluates routes top-to-bottom and returns the first matching
// route config. Returns nil if no route matches.
func MatchRoute(obj *InventoryObject, routes []config.SyncRouteConfig) *config.SyncRouteConfig {
	for i := range routes {
		if routeMatches(obj, &routes[i]) {
			return &routes[i]
		}
	}
	return nil
}

// routeMatches checks if a single route matches the object.
// All fields in Match are AND'd; multiple values per field are OR'd.
func routeMatches(obj *InventoryObject, route *config.SyncRouteConfig) bool {
	for field, values := range route.Match {
		if len(values) == 0 {
			continue
		}
		if !fieldMatches(obj, field, values) {
			return false // AND: any field mismatch means no match
		}
	}
	return true
}

// fieldMatches checks if the object's field value is in the allowed set (OR).
func fieldMatches(obj *InventoryObject, field string, values []string) bool {
	// Check normalized top-level fields first
	if getter, ok := normalizedFields[field]; ok {
		actual := getter(obj)
		for _, v := range values {
			if actual == v {
				return true
			}
		}
		return false
	}

	// Fall through to attributes
	if obj.Attributes == nil {
		return false
	}
	attrValues, ok := obj.Attributes[field]
	if !ok {
		return false
	}
	for _, allowed := range values {
		for _, actual := range attrValues {
			if actual == allowed {
				return true
			}
		}
	}
	return false
}

// ResolveDestination returns the context and include file for a matched route.
// If IncludeFile is empty in the route config, the default sync-owned
// include file is derived from the source name.
func ResolveDestination(route *config.SyncRouteConfig, sourceName string) (context, includeFile string) {
	context = route.Context
	includeFile = route.IncludeFile
	if includeFile == "" {
		includeFile = fmt.Sprintf("conf.d/sync_%s", sourceName)
	}
	return context, includeFile
}
