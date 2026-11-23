package repl

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Never reach a real Rex when tests run inside one.
	_ = os.Setenv("NSSH_REX", "0")
	os.Exit(m.Run())
}
