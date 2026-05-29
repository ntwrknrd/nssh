package selection

import "testing"

func TestSelectorMatchesPlainAndFieldTerms(t *testing.T) {
	selector, err := Compile("group:cbb user:chris core", []string{"host", "hostname", "user", "provider", "group", "port"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	matches := selector.Match(Row{
		"host":     "151-core1",
		"hostname": "151-core1.expedient.com",
		"user":     "chris",
		"provider": "local",
		"group":    "cbb",
		"port":     "22",
	})
	if !matches {
		t.Fatal("expected row to match")
	}

	wrongGroup := selector.Match(Row{
		"host":     "151-core1",
		"hostname": "151-core1.expedient.com",
		"user":     "chris",
		"provider": "local",
		"group":    "custcbb",
		"port":     "22",
	})
	if wrongGroup {
		t.Fatal("expected exact group selector not to match custcbb")
	}
}

func TestSelectorTreatsUnknownFieldSyntaxAsPlainSearch(t *testing.T) {
	selector, err := Compile("2001:db8", []string{"host", "hostname"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !selector.Match(Row{"host": "v6-router", "hostname": "2001:db8::1"}) {
		t.Fatal("expected IPv6-looking plain term to match")
	}
}

func TestSelectorRejectsInvalidRegex(t *testing.T) {
	_, err := Compile("[", []string{"host"})
	if err == nil {
		t.Fatal("expected invalid regex error")
	}
}
