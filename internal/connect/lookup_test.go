package connect

import (
	"errors"
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
