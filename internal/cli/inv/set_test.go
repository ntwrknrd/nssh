package inv

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestBuildSetAuthPatchInfersPasswordRef(t *testing.T) {
	patch, err := buildSetAuthPatch("", "pass", "edge01", false)
	if err != nil {
		t.Fatalf("buildSetAuthPatch: %v", err)
	}
	if patch.Auth.Mode != config.AuthModePassword || patch.Auth.CredentialProvider != "pass" || patch.Auth.PasswordRef != "nssh/hosts/edge01" {
		t.Fatalf("patch = %+v", patch)
	}

	patch, err = buildSetAuthPatch("password", "op-expedient:op://Network/Edge/password", "netbox-prod/custcbb", true)
	if err != nil {
		t.Fatalf("buildSetAuthPatch group: %v", err)
	}
	if patch.Auth.CredentialProvider != "op-expedient" || patch.Auth.PasswordRef != "op://Network/Edge/password" {
		t.Fatalf("group patch = %+v", patch)
	}
}

func TestResolveSetTargetClassifiesGroupAndHostTargets(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		group    string
		hostFlag bool
		want     setTarget
		wantErr  bool
	}{
		{name: "group flag without arg", group: "local/default", want: setTarget{Kind: setTargetGroup, Value: "local/default"}},
		{name: "slash arg is group", args: []string{"netbox-prod/custcbb"}, want: setTarget{Kind: setTargetGroup, Value: "netbox-prod/custcbb"}},
		{name: "slash arg with host flag remains host", args: []string{"edge01/site"}, hostFlag: true, want: setTarget{Kind: setTargetHost, Value: "edge01/site"}},
		{name: "plain arg is host", args: []string{"edge01"}, want: setTarget{Kind: setTargetHost, Value: "edge01"}},
		{name: "missing host", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSetTarget(tt.args, tt.group, tt.group != "", tt.hostFlag)
			if tt.wantErr {
				if err == nil {
					t.Fatal("error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSetTarget: %v", err)
			}
			if got != tt.want {
				t.Fatalf("target = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBuildSetAuthPatchCredNoneClearsOrSetsKeyMode(t *testing.T) {
	patch, err := buildSetAuthPatch("", "none", "edge01", false)
	if err != nil {
		t.Fatalf("buildSetAuthPatch clear: %v", err)
	}
	if !patch.Clear {
		t.Fatalf("patch = %+v, want clear", patch)
	}

	patch, err = buildSetAuthPatch("key", "none", "edge01", false)
	if err != nil {
		t.Fatalf("buildSetAuthPatch key: %v", err)
	}
	if patch.Clear || patch.Auth.Mode != config.AuthModeKey || patch.Auth.CredentialProvider != "" || patch.Auth.PasswordRef != "" {
		t.Fatalf("patch = %+v, want key mode without credential", patch)
	}
}

func TestRunSetGroupAppliesCompactCredentialFlags(t *testing.T) {
	if os.Getenv("NSSH_TEST_RUN_SET_GROUP") == "1" {
		runSetGroupCompactCredentialHelper(t)
		return
	}

	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestRunSetGroupAppliesCompactCredentialFlags$")
	cmd.Env = append(os.Environ(),
		"NSSH_TEST_RUN_SET_GROUP=1",
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"),
		"XDG_STATE_HOME="+filepath.Join(home, "state"),
		"PATH="+t.TempDir(),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, output)
	}
}

func runSetGroupCompactCredentialHelper(t *testing.T) {
	t.Helper()

	home := os.Getenv("HOME")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("PATH", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.Credential.Provider["op-expedient"] = config.CredentialProviderConfig{
		Type: config.CredentialProvider1Password,
		Config: config.CredentialProviderDetailConfig{
			Vault: "Network",
		},
	}
	cfg.Inventory.Provider["netbox-prod"] = config.InventoryProviderConfig{
		Type:  config.ProviderNetBox,
		Group: map[string]config.GroupConfig{"custcbb": {}},
	}
	paths := config.DefaultPaths()
	if err := os.MkdirAll(paths.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(paths.ConfigFile, cfg); err != nil {
		t.Fatal(err)
	}

	patch, err := buildSetAuthPatch("password", "op-expedient:op://Network/Edge/password", "netbox-prod/custcbb", true)
	if err != nil {
		t.Fatalf("buildSetAuthPatch: %v", err)
	}
	if err := runSetGroup("netbox-prod/custcbb", patch); err != nil {
		t.Fatalf("runSetGroup: %v", err)
	}

	loaded, err := config.Load(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	auth := loaded.Inventory.Provider["netbox-prod"].Group["custcbb"].Auth
	if auth.Mode != config.AuthModePassword || auth.CredentialProvider != "op-expedient" || auth.PasswordRef != "op://Network/Edge/password" {
		t.Fatalf("group auth = %+v", auth)
	}
}

func TestPromptGroupAuthPatchUsesCredentialItemPicker(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"pass": {Type: config.CredentialProviderPass},
	}

	oldList := listCredentialItems
	listCredentialItems = func(*config.Config, string) ([]credentialItem, error) {
		return []credentialItem{{Label: "default group", Ref: "nssh/groups/local/default"}}, nil
	}
	defer func() { listCredentialItems = oldList }()

	prompter := &fakeLocalHostAddPrompter{
		selects: map[string]string{
			"Authentication":      config.AuthModePassword,
			"Credential provider": "pass",
			"Credential item":     "nssh/groups/local/default",
		},
	}
	patch, err := promptGroupAuthPatchWithPrompter(cfg, "local/default", prompter)
	if err != nil {
		t.Fatalf("promptGroupAuthPatchWithPrompter: %v", err)
	}
	if patch.Auth.Mode != config.AuthModePassword || patch.Auth.CredentialProvider != "pass" || patch.Auth.PasswordRef != "nssh/groups/local/default" {
		t.Fatalf("patch = %+v", patch)
	}
}
