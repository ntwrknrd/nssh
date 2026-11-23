package connect

import (
	"reflect"
	"testing"
)

func newShortNameCatalog(preferred []string, hosts ...*ResolvedHostData) *HostCatalog {
	cat := &HostCatalog{hosts: map[string]*ResolvedHostData{}, aliases: map[string]string{}, preferred: preferred}
	for _, host := range hosts {
		cat.add(host)
	}
	cat.preferShortNames()
	return cat
}

func TestShortNameSkipsIPAddresses(t *testing.T) {
	for in, want := range map[string]string{
		"core1.lab.local":  "core1",
		"core1.lab.local.": "core1",
		"core1":            "",
		"172.16.1.5":       "",
		"10.0.0.1.":        "",
		"::ffff:10.0.0.1":  "",
		"fe80::1":          "",
	} {
		if got := shortName(in); got != want {
			t.Errorf("shortName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCatalogIPHostnamesAddNoShortName(t *testing.T) {
	cat := newShortNameCatalog(nil,
		&ResolvedHostData{Canonical: "spine1", Hostname: "172.16.1.5"},
		&ResolvedHostData{Canonical: "spine2", Hostname: "172.16.1.6"},
	)
	if host, ok := cat.Find("172"); ok {
		t.Fatalf("Find(172) = %s, want no match", host.Canonical)
	}
	if _, ok := cat.shortClaims["172"]; ok {
		t.Fatal("172 recorded as a shared short name")
	}
	if host, ok := cat.Find("172.16.1.5"); !ok || host.Canonical != "spine1" {
		t.Fatalf("Find(172.16.1.5) = %v, %v", host, ok)
	}
}

func TestCatalogPreferredDomainsResolveSharedShortNames(t *testing.T) {
	internal := &ResolvedHostData{Canonical: "acm-core1.custcbb.local", Hostname: "acm-core1.custcbb.local"}
	public := &ResolvedHostData{Canonical: "acm-core1.expedient.com", Hostname: "acm-core1.expedient.com"}
	copyOf := func(h *ResolvedHostData) *ResolvedHostData { c := *h; return &c }
	for name, order := range map[string][]*ResolvedHostData{
		"internal first": {copyOf(internal), copyOf(public)},
		"public first":   {copyOf(public), copyOf(internal)},
	} {
		t.Run(name, func(t *testing.T) {
			cat := newShortNameCatalog([]string{"custcbb.local", "expedient.com"}, order...)
			if host, ok := cat.Find("acm-core1"); !ok || host.Canonical != "acm-core1.custcbb.local" {
				t.Fatalf("Find(acm-core1) = %v, %v", host, ok)
			}
			if host, ok := cat.Find("acm-core1.expedient.com"); !ok || host.Canonical != "acm-core1.expedient.com" {
				t.Fatalf("full name lookup = %v, %v", host, ok)
			}
			want := []string{"acm-core1.custcbb.local", "acm-core1.expedient.com"}
			if got := cat.Suggestions("acm-core"); !reflect.DeepEqual(got, want) {
				t.Fatalf("Suggestions = %v, want %v", got, want)
			}
		})
	}
	// Suggestions put preferred domains first even against name order.
	cat := newShortNameCatalog([]string{"zeta.net"},
		&ResolvedHostData{Canonical: "edge1.alpha.net"},
		&ResolvedHostData{Canonical: "edge1.zeta.net"},
	)
	if got := cat.Suggestions("edge1"); !reflect.DeepEqual(got, []string{"edge1.zeta.net", "edge1.alpha.net"}) {
		t.Fatalf("Suggestions = %v", got)
	}
}

func TestCatalogPreferredDomainsKeepAmbiguity(t *testing.T) {
	tests := map[string]struct {
		preferred []string
		hosts     []string
	}{
		"no preference":      {nil, []string{"core1.a.net", "core1.b.net"}},
		"neither listed":     {[]string{"c.net"}, []string{"core1.a.net", "core1.b.net"}},
		"two in best domain": {[]string{"a.net"}, []string{"core1.a.net", "core1.x.a.net", "core1.b.net"}},
		"label boundary":     {[]string{"a.net"}, []string{"core1.xa.net", "core1.b.net"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var hosts []*ResolvedHostData
			for _, h := range tt.hosts {
				hosts = append(hosts, &ResolvedHostData{Canonical: h})
			}
			cat := newShortNameCatalog(tt.preferred, hosts...)
			if host, ok := cat.Find("core1"); ok {
				t.Fatalf("Find(core1) = %s, want ambiguous", host.Canonical)
			}
		})
	}
}

func TestCatalogPreferredDomainsDoNotOverrideAliases(t *testing.T) {
	cat := newShortNameCatalog([]string{"a.net"},
		&ResolvedHostData{Canonical: "core1.a.net"},
		&ResolvedHostData{Canonical: "core1.b.net"},
		&ResolvedHostData{Canonical: "spare.b.net", Aliases: []string{"core1"}},
	)
	if host, ok := cat.Find("core1"); !ok || host.Canonical != "spare.b.net" {
		t.Fatalf("Find(core1) = %v, %v, want the explicit alias", host, ok)
	}
}
