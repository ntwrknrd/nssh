package connect

import (
	"errors"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
)

func TestHostNotFoundErrorCarriesHostname(t *testing.T) {
	var err error = &HostNotFoundError{Hostname: "edge01"}
	var notFound *HostNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatal("HostNotFoundError should support errors.As")
	}
	if notFound.Hostname != "edge01" || notFound.Error() != "host not found: edge01" {
		t.Fatalf("notFound = %+v error=%q", notFound, notFound.Error())
	}
}

func TestIsCompatibilityError(t *testing.T) {
	if !isCompatibilityError(&exit.ExitError{Code: exit.ExitConnectionFailed}) {
		t.Fatal("connection failed exit should be compatibility candidate")
	}
	if !isCompatibilityError(&exit.ExitError{Code: 255}) {
		t.Fatal("ssh exit 255 should be compatibility candidate")
	}
	if isCompatibilityError(&exit.ExitError{Code: exit.ExitAuthFailed}) {
		t.Fatal("auth failure should not be compatibility candidate")
	}
	if isCompatibilityError(errors.New("plain error")) {
		t.Fatal("plain error should not be compatibility candidate")
	}
}

func TestExtractExplicitUser(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		sshArgs  []string
		want     string
	}{
		{name: "user at host", hostname: "admin@edge01", want: "admin"},
		{name: "split login flag", hostname: "edge01", sshArgs: []string{"-l", "admin"}, want: "admin"},
		{name: "joined login flag", hostname: "edge01", sshArgs: []string{"-ladmin"}, want: "admin"},
		{name: "no explicit user", hostname: "edge01", sshArgs: []string{"-p", "2222"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractExplicitUser(tt.hostname, tt.sshArgs); got != tt.want {
				t.Fatalf("extractExplicitUser() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppendResolvedHostCompatFixesCreatesProviderHostOverlay(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"netbox-prod": {
			Type:   config.ProviderNetBox,
			Groups: map[string]config.GroupConfig{"cbb": {}},
		},
	}
	cfg.Inventory.Provider = cfg.Inventory.Providers
	resolved := &ResolvedHost{
		Canonical: "701-sw37r103c608.expedient.com",
		Hostname:  "701-sw37r103c608.expedient.com",
		Provider:  "netbox-prod",
		Group:     "cbb",
	}

	if err := appendResolvedHostCompatFixes(cfg, resolved, []compat.FloorSelection{
		{Category: compat.CategoryKex, Directive: "KexAlgorithms", Floor: "diffie-hellman-group14-sha1"},
		{Category: compat.CategoryKex, Directive: "KexAlgorithms", Floor: "diffie-hellman-group14-sha1"},
	}); err != nil {
		t.Fatalf("appendResolvedHostCompatFixes: %v", err)
	}

	host := cfg.Inventory.Providers["netbox-prod"].Hosts["701-sw37r103c608.expedient.com"]
	if host.Group != "cbb" {
		t.Fatalf("host group = %q, want cbb", host.Group)
	}
	if got := host.SSH.Compatibility.Kex; got != "diffie-hellman-group14-sha1" {
		t.Fatalf("host compatibility.kex = %q, want group14", got)
	}
}

func TestCompatibilityFixesApplyWithHardenedAlgorithmDefaults(t *testing.T) {
	sshConfig := config.SSHHostConfig{
		Options: config.SSHOptions{
			"KexAlgorithms": config.NewSSHOptionItems(
				"sntrup761x25519-sha512@openssh.com",
				"curve25519-sha256",
				"curve25519-sha256@libssh.org",
			),
		},
	}
	output := "Unable to negotiate with 192.0.2.1 port 22: no matching key exchange method found. Their offer: diffie-hellman-group14-sha1,diffie-hellman-group1-sha1"

	fixes := compatibilityFixesToApply(sshConfig, output)

	if len(fixes) != 1 {
		t.Fatalf("fixes = %#v, want one kex fix", fixes)
	}
	if fixes[0].Category != compat.CategoryKex || fixes[0].Floor != "diffie-hellman-group14-sha1" {
		t.Fatalf("fix = %#v, want kex group14 floor", fixes[0])
	}
}
