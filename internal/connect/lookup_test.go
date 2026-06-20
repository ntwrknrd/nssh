package connect

import (
	"errors"
	"reflect"
	"testing"
)

func TestResolveHostnameFromCatalogAutoSelectsSingleSuggestion(t *testing.T) {
	const host = "810-cactimain01.ldap.custcbb.local"
	catalog := &HostCatalog{
		hosts: map[string]*ResolvedHostData{
			host: {Canonical: host},
		},
		aliases: map[string]string{},
	}

	got, err := resolveHostnameFromCatalog("cacti", catalog, func(string, []string, string) (string, error) {
		t.Fatal("selector should not open for one suggestion")
		return "", nil
	})
	if err != nil {
		t.Fatalf("resolveHostnameFromCatalog: %v", err)
	}
	if got != host {
		t.Fatalf("resolved hostname = %q, want %q", got, host)
	}
}

func TestResolveHostnameFromCatalogAutoSelectsSingleAliasSuggestion(t *testing.T) {
	const host = "clab-dfz-core01"
	catalog := &HostCatalog{
		hosts: map[string]*ResolvedHostData{
			host: {
				Canonical: host,
				Hostname:  "172.20.20.13",
				Aliases:   []string{"dfz-core01"},
			},
		},
		aliases: map[string]string{
			host:         host,
			"dfz-core01": host,
		},
	}

	got, err := resolveHostnameFromCatalog("dfz", catalog, func(string, []string, string) (string, error) {
		t.Fatal("selector should not open for one alias-backed suggestion")
		return "", nil
	})
	if err != nil {
		t.Fatalf("resolveHostnameFromCatalog: %v", err)
	}
	if got != host {
		t.Fatalf("resolved hostname = %q, want %q", got, host)
	}
}

func TestResolveHostnameFromCatalogSelectsFromMultipleAliasSuggestions(t *testing.T) {
	catalog := &HostCatalog{
		hosts: map[string]*ResolvedHostData{
			"clab-dfz-core01": {
				Canonical: "clab-dfz-core01",
				Hostname:  "172.20.20.13",
				Aliases:   []string{"dfz-core01"},
			},
			"clab-dfz-core02": {
				Canonical: "clab-dfz-core02",
				Hostname:  "172.20.20.14",
				Aliases:   []string{"dfz-core02"},
			},
		},
		aliases: map[string]string{
			"clab-dfz-core01": "clab-dfz-core01",
			"dfz-core01":      "clab-dfz-core01",
			"clab-dfz-core02": "clab-dfz-core02",
			"dfz-core02":      "clab-dfz-core02",
		},
	}

	got, err := resolveHostnameFromCatalog("dfz", catalog, func(prompt string, options []string, initialQuery string) (string, error) {
		if prompt != "Select host" {
			t.Fatalf("prompt = %q, want Select host", prompt)
		}
		wantOptions := []string{"clab-dfz-core01", "clab-dfz-core02"}
		if !reflect.DeepEqual(options, wantOptions) {
			t.Fatalf("options = %#v, want %#v", options, wantOptions)
		}
		if initialQuery != "dfz" {
			t.Fatalf("initialQuery = %q, want dfz", initialQuery)
		}
		return "clab-dfz-core02", nil
	})
	if err != nil {
		t.Fatalf("resolveHostnameFromCatalog: %v", err)
	}
	if got != "clab-dfz-core02" {
		t.Fatalf("resolved hostname = %q, want clab-dfz-core02", got)
	}
}

func TestResolveHostnameMissReturnsHostNotFound(t *testing.T) {
	_, err := ResolveHostname("codex-no-such-host-refresh-regression")
	var notFound *HostNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("ResolveHostname error = %v, want HostNotFoundError", err)
	}
	if notFound.Hostname != "codex-no-such-host-refresh-regression" {
		t.Fatalf("not found hostname = %q", notFound.Hostname)
	}
}
