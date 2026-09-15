package repl

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHistoryAppendLoadAndPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "repl_history")
	h := historyStore{path: path}
	if err := h.append("[ 'a' ] ( 'show version' )"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := h.load()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"[ 'a' ] ( 'show version' )"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("history=%q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}
func TestHistoryLoadReadsBoundedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repl_history")
	old := make([]byte, maxHistoryBytes+100)
	for i := range old {
		old[i] = 'x'
	}
	old = append(old, []byte("\nnewest\n")...)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := historyStore{path: path}.load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"newest"}) {
		t.Fatalf("history=%q", got)
	}
}
func TestBoundedHistoryDropsOldEntries(t *testing.T) {
	entries := make([]string, maxHistoryEntries+1)
	for i := range entries {
		entries[i] = "command"
	}
	if got := boundedHistory(entries); len(got) != maxHistoryEntries {
		t.Fatalf("length=%d", len(got))
	}
}

func TestOversizedSubmissionDoesNotEraseHistory(t *testing.T) {
	h := historyStore{path: filepath.Join(t.TempDir(), "history")}
	if err := h.append("keep this"); err != nil {
		t.Fatal(err)
	}
	if err := h.append(strings.Repeat("x", maxHistoryBytes)); err == nil {
		t.Fatal("oversized history entry accepted")
	}
	got, err := h.load()
	if err != nil || !reflect.DeepEqual(got, []string{"keep this"}) {
		t.Fatalf("history=%v err=%v", got, err)
	}
}
