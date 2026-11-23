package connect

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "nssh-connect-test-*")
	if err != nil {
		panic(err)
	}
	// Never reach a real Rex when tests run inside one.
	_ = os.Setenv("NSSH_REX", "0")
	_ = os.Setenv("XDG_CONFIG_HOME", tmp)
	_ = os.Setenv("XDG_DATA_HOME", tmp)
	_ = os.Setenv("XDG_STATE_HOME", tmp)

	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}
