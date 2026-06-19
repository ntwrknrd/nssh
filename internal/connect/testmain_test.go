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
	_ = os.Setenv("XDG_CONFIG_HOME", tmp)
	_ = os.Setenv("XDG_DATA_HOME", tmp)
	_ = os.Setenv("XDG_STATE_HOME", tmp)

	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}
