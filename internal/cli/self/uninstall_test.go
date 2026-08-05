package self

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunUninstallRemovesAskpassBesideBinary(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", binDir)
	for _, name := range []string{"nssh", "nssh-askpass"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("binary\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}

	if err := runUninstall(true, true, false, true); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	for _, name := range []string{"nssh", "nssh-askpass"} {
		if _, err := os.Stat(filepath.Join(binDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after uninstall: %v", name, err)
		}
	}
}
