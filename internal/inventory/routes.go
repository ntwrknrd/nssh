package inventory

import (
	"path"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
)

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

// BestGroupSelectorMatch chooses a single best match when specificity is
// obvious. Ambiguous equal-specificity matches are left for the caller.
func BestGroupSelectorMatch(obj *Object, matches []config.InventoryGroupSelector) (config.InventoryGroupSelector, bool) {
	switch len(matches) {
	case 0:
		return config.InventoryGroupSelector{}, false
	case 1:
		return matches[0], true
	}

	var best config.InventoryGroupSelector
	bestScore := 0
	tied := false
	for _, selector := range matches {
		score := domainSuffixSelectorScore(obj, selector)
		switch {
		case score == 0:
			continue
		case score > bestScore:
			best = selector
			bestScore = score
			tied = false
		case score == bestScore:
			tied = true
		}
	}
	if bestScore == 0 || tied {
		return config.InventoryGroupSelector{}, false
	}
	return best, true
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
	if field == "domain_suffix" {
		return domainSuffixFieldMatches(obj, values)
	}
	if getter, ok := normalizedFields[field]; ok {
		actual := getter(obj)
		for _, allowed := range values {
			if valueMatches(actual, allowed) {
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
			if valueMatches(actual, allowed) {
				return true
			}
		}
	}
	return false
}

func valueMatches(actual, allowed string) bool {
	actual = strings.TrimSpace(actual)
	allowed = strings.TrimSpace(allowed)
	if actual == "" || allowed == "" {
		return false
	}
	if actual == allowed {
		return true
	}
	if strings.ContainsAny(allowed, "*?[") {
		matched, err := path.Match(allowed, actual)
		return err == nil && matched
	}
	return false
}

func domainSuffixFieldMatches(obj *Object, values []string) bool {
	actualValues := domainSuffixValues(obj)
	for _, allowed := range values {
		for _, actual := range actualValues {
			if domainSuffixValueMatches(actual, allowed) {
				return true
			}
		}
	}
	return false
}

func domainSuffixSelectorScore(obj *Object, selector config.InventoryGroupSelector) int {
	values := selector.Match["domain_suffix"]
	if len(values) == 0 {
		return 0
	}
	actualValues := domainSuffixValues(obj)
	best := 0
	for _, allowed := range values {
		for _, actual := range actualValues {
			if !domainSuffixValueMatches(actual, allowed) {
				continue
			}
			if score := domainSuffixSpecificity(allowed); score > best {
				best = score
			}
		}
	}
	return best
}

func domainSuffixValues(obj *Object) []string {
	if obj == nil {
		return nil
	}
	values := make([]string, 0, 4)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		for _, existing := range values {
			if existing == value {
				return
			}
		}
		values = append(values, value)
	}
	if obj.Attributes != nil {
		for _, value := range obj.Attributes["domain_suffix"] {
			add(value)
		}
	}
	add(domainSuffixFromHost(obj.FQDN))
	add(domainSuffixFromHost(obj.HostName))
	add(domainSuffixFromHost(obj.Name))
	return values
}

func domainSuffixFromHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	dot := strings.Index(host, ".")
	if dot <= 0 || dot == len(host)-1 {
		return ""
	}
	return host[dot:]
}

func domainSuffixValueMatches(actual, allowed string) bool {
	actual = strings.ToLower(strings.TrimSpace(actual))
	allowed = strings.ToLower(strings.TrimSpace(allowed))
	if actual == "" || allowed == "" {
		return false
	}
	if valueMatches(actual, allowed) {
		return true
	}
	if strings.HasPrefix(allowed, "*") {
		suffix := strings.TrimPrefix(allowed, "*")
		return suffix != "" && strings.HasSuffix(actual, suffix)
	}
	return strings.HasSuffix(actual, allowed)
}

func domainSuffixSpecificity(value string) int {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimLeft(value, "*")
	value = strings.TrimLeft(value, ".")
	return len(value)
}
