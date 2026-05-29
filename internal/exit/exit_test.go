package exit

import (
	"os"
	"strings"
	"testing"
)

func TestExitPackageDoesNotExposeVaultErrors(t *testing.T) {
	data, err := os.ReadFile("exit.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, reject := range []string{"ExitVaultError", "ErrVault", "vault error"} {
		if strings.Contains(text, reject) {
			t.Fatalf("exit package should not expose stale vault symbol %q", reject)
		}
	}
}
