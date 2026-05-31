package inventory

import "github.com/ntwrknrd/nssh/internal/config"

var normalizedFields = map[string]func(*Object) string{
	"provider":    func(o *Object) string { return o.Provider },
	"object_type": func(o *Object) string { return o.ObjectType },
	"name":        func(o *Object) string { return o.Name },
	"fqdn":        func(o *Object) string { return o.FQDN },
	"hostname":    func(o *Object) string { return o.HostName },
}

// MatchGroupSelectors returns every provider group selector that matches obj.
func MatchGroupSelectors(obj *Object, selectors []config.InventoryGroupSelector) []config.InventoryGroupSelector {
	matches := make([]config.InventoryGroupSelector, 0, 1)
	for i := range selectors {
		if selectorMatches(obj, &selectors[i]) {
			matches = append(matches, selectors[i])
		}
	}
	return matches
}

func selectorMatches(obj *Object, selector *config.InventoryGroupSelector) bool {
	for field, values := range selector.Match {
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
