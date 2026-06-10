package repl

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileHistoryStoreLoadMissingFileReturnsEmpty(t *testing.T) {
	store := &FileHistoryStore{Path: filepath.Join(t.TempDir(), "history")}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("history = %#v, want empty", got)
	}
}

func TestFileHistoryStoreAppendAndLoad(t *testing.T) {
	store := &FileHistoryStore{Path: filepath.Join(t.TempDir(), "state", "repl_history")}

	if err := store.Append("[ 'edge01' ] ( 'show hostname' )"); err != nil {
		t.Fatalf("Append first: %v", err)
	}
	if err := store.Append("[ 'edge02' ] ( 'show version' )"); err != nil {
		t.Fatalf("Append second: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"[ 'edge01' ] ( 'show hostname' )", "[ 'edge02' ] ( 'show version' )"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %#v, want %#v", got, want)
	}
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatalf("stat history: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("history perm = %v, want 0600", info.Mode().Perm())
	}
}

func TestFileHistoryStoreLoadAppliesLimit(t *testing.T) {
	store := &FileHistoryStore{Path: filepath.Join(t.TempDir(), "history"), Limit: 2}
	for _, line := range []string{"one", "two", "three"} {
		if err := store.Append(line); err != nil {
			t.Fatalf("Append %q: %v", line, err)
		}
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"two", "three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %#v, want %#v", got, want)
	}
}
