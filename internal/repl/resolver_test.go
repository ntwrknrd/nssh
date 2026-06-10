package repl

import (
	"reflect"
	"testing"

	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

func TestSuggestableHostNamesAddsShortAliasForFQDNPattern(t *testing.T) {
	host := &sshconfig.HostEntry{
		Host:     "acm-lab-agg-sw1.custcbb.local",
		Patterns: []string{"acm-lab-agg-sw1.custcbb.local"},
	}

	got := suggestableHostNames(host)
	want := []string{"acm-lab-agg-sw1.custcbb.local", "acm-lab-agg-sw1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %#v, want %#v", got, want)
	}
}

func TestSuggestableHostNamesDoesNotShortenIPAddresses(t *testing.T) {
	host := &sshconfig.HostEntry{
		Host:     "192.0.2.10",
		Patterns: []string{"192.0.2.10"},
	}

	got := suggestableHostNames(host)
	want := []string{"192.0.2.10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %#v, want %#v", got, want)
	}
}

func TestShortestHostSuggestionsPrefersShortAliasOverFQDN(t *testing.T) {
	got := shortestHostSuggestions([]string{
		"acm-lab-agg-sw1.custcbb.local",
		"acm-lab-agg-sw1",
		"acm-lab-border-sw1",
	})
	want := []string{"acm-lab-agg-sw1", "acm-lab-border-sw1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestions = %#v, want %#v", got, want)
	}
}
