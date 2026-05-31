package inv

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestListOnePasswordCredentialItemsUsesConfiguredScopeAndIDs(t *testing.T) {
	old := runCredentialProviderCommand
	defer func() { runCredentialProviderCommand = old }()
	var gotCommand string
	var gotArgs []string
	runCredentialProviderCommand = func(_ context.Context, command string, _ []byte, args ...string) ([]byte, error) {
		gotCommand = command
		gotArgs = args
		return []byte(`[{"id":"abc123","title":"edge01","category":"LOGIN"},{"id":"def456","title":"edge02","category":"LOGIN"}]`), nil
	}

	items, err := listOnePasswordCredentialItems(config.CredentialProviderConfig{
		Config: config.CredentialProviderDetailConfig{Vault: "Network", Account: "expedient"},
	})
	if err != nil {
		t.Fatalf("listOnePasswordCredentialItems: %v", err)
	}
	if gotCommand != "op" {
		t.Fatalf("command = %q, want op", gotCommand)
	}
	if strings.Join(gotArgs, " ") != "item list --vault Network --account expedient --format json" {
		t.Fatalf("args = %q", strings.Join(gotArgs, " "))
	}
	want := []credentialItem{
		{Label: "edge01 (LOGIN)", Ref: "op://Network/abc123/"},
		{Label: "edge02 (LOGIN)", Ref: "op://Network/def456/"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("items = %+v, want %+v", items, want)
	}
}

func TestParsePassCredentialItemsFromTree(t *testing.T) {
	output := strings.Join([]string{
		"nssh",
		"\u251c\u2500\u2500 groups",
		"\u2502   \u2514\u2500\u2500 lab",
		"\u2514\u2500\u2500 hosts",
		"    \u251c\u2500\u2500 edge01",
		"    \u2514\u2500\u2500 edge02",
	}, "\n")

	items := parsePassCredentialItems("nssh", output)
	want := []credentialItem{
		{Label: "nssh/groups/lab", Ref: "nssh/groups/lab"},
		{Label: "nssh/hosts/edge01", Ref: "nssh/hosts/edge01"},
		{Label: "nssh/hosts/edge02", Ref: "nssh/hosts/edge02"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("items = %+v, want %+v", items, want)
	}
}
