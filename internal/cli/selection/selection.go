package selection

import (
	"regexp"
	"strings"
)

// Row is the field/value set a selector evaluates.
type Row map[string]string

// Selector matches rows using AND across all terms.
type Selector struct {
	fields map[string]bool
	terms  []term
}

type term struct {
	field string
	exact string
	re    *regexp.Regexp
}

// Compile parses a select expression.
//
// Plain terms are case-insensitive regexes matched across all fields.
// field:value terms are exact, case-insensitive matches for recognized fields.
func Compile(query string, fields []string) (*Selector, error) {
	selector := &Selector{
		fields: make(map[string]bool, len(fields)),
	}
	for _, field := range fields {
		selector.fields[strings.ToLower(strings.TrimSpace(field))] = true
	}
	for _, raw := range strings.Fields(query) {
		field, value, hasField := strings.Cut(raw, ":")
		field = strings.ToLower(strings.TrimSpace(field))
		if hasField && selector.fields[field] {
			selector.terms = append(selector.terms, term{field: field, exact: value})
			continue
		}
		re, err := regexp.Compile("(?i)" + raw)
		if err != nil {
			return nil, err
		}
		selector.terms = append(selector.terms, term{re: re})
	}
	return selector, nil
}

// Match reports whether row satisfies all selector terms.
func (s *Selector) Match(row Row) bool {
	if s == nil {
		return true
	}
	for _, term := range s.terms {
		if term.field != "" {
			if !strings.EqualFold(row[term.field], term.exact) {
				return false
			}
			continue
		}
		if !s.matchAnyField(row, term.re) {
			return false
		}
	}
	return true
}

func (s *Selector) matchAnyField(row Row, re *regexp.Regexp) bool {
	if re == nil {
		return true
	}
	for field := range s.fields {
		if re.MatchString(row[field]) {
			return true
		}
	}
	return false
}
