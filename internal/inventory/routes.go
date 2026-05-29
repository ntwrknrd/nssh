package inventory

import "github.com/ntwrknrd/nssh/internal/config"

var normalizedFields = map[string]func(*Object) string{
	"provider":    func(o *Object) string { return o.Provider },
	"object_type": func(o *Object) string { return o.ObjectType },
	"name":        func(o *Object) string { return o.Name },
	"fqdn":        func(o *Object) string { return o.FQDN },
	"hostname":    func(o *Object) string { return o.HostName },
}

// MatchRoute evaluates routes top-to-bottom and returns the first match.
func MatchRoute(obj *Object, routes []config.InventoryRouteConfig) *config.InventoryRouteConfig {
	for i := range routes {
		if routeMatches(obj, &routes[i]) {
			return &routes[i]
		}
	}
	return nil
}

func routeMatches(obj *Object, route *config.InventoryRouteConfig) bool {
	for field, values := range route.Match {
		if len(values) == 0 {
			continue
		}
		if !fieldMatches(obj, field, values) {
			return false
		}
	}
	return true
}

func fieldMatches(obj *Object, field string, values []string) bool {
	if getter, ok := normalizedFields[field]; ok {
		actual := getter(obj)
		for _, allowed := range values {
			if actual == allowed {
				return true
			}
		}
		return false
	}
	if obj.Attributes == nil {
		return false
	}
	actualValues, ok := obj.Attributes[field]
	if !ok {
		return false
	}
	for _, allowed := range values {
		for _, actual := range actualValues {
			if actual == allowed {
				return true
			}
		}
	}
	return false
}
