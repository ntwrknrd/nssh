package inv

import "testing"

func TestListCommandDoesNotRegisterGroupsFlag(t *testing.T) {
	cmd := newListCmd()
	if flag := cmd.Flags().Lookup("groups"); flag != nil {
		t.Fatal("list command should not register --groups")
	}
	if flag := cmd.Flags().ShorthandLookup("g"); flag != nil {
		t.Fatal("list command should not register -g")
	}
}

func TestGetCommandDoesNotRegisterGroupFlag(t *testing.T) {
	cmd := newGetCmd()
	if flag := cmd.Flags().Lookup("group"); flag != nil {
		t.Fatal("get command should not register --group")
	}
	if flag := cmd.Flags().ShorthandLookup("g"); flag != nil {
		t.Fatal("get command should not register -g")
	}
}
