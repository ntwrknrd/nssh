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
