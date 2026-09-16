package repl

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestClearPreservesHistoryAndCtrlKPreservesDraft(t *testing.T) {
	for _, command := range []string{":clear", "ctrl-k", "ctrl-l"} {
		t.Run(command, func(t *testing.T) {
			m := testTUI(80)
			m.entries = []string{"saved"}
			m.history = historyStore{path: filepath.Join(t.TempDir(), "history")}
			if err := m.history.append("saved"); err != nil {
				t.Fatal(err)
			}
			m.acceptResult(tuiEvent("a", "show", "output", 0))
			m.transcript.truncated = true
			m.selected = true
			m.controlOpen = true
			m.controlInput.SetValue(command)
			key := tea.KeyEnter
			if command != ":clear" {
				key = tea.KeyCtrlK
				if command == "ctrl-l" {
					key = tea.KeyCtrlL
				}
				m.input.SetValue("draft")
				m.active = true
			}
			next, _ := m.Update(tea.KeyMsg{Type: key})
			m = next.(model)
			if len(m.blocks) != 0 || m.bytes != 0 || m.transcript.truncated || m.selected || m.commandHeader() != "" {
				t.Fatal("display retained")
			}
			got, err := m.history.load()
			if err != nil || !reflect.DeepEqual(got, []string{"saved"}) || !reflect.DeepEqual(m.entries, got) {
				t.Fatal("history changed")
			}
			if command == "ctrl-k" && (m.input.Value() != "draft" || !m.active) {
				t.Fatal("draft/work changed")
			}
			m.acceptResult(tuiEvent("a", "next", "new output", 0))
			if len(m.blocks) == 0 {
				t.Fatal("new output discarded")
			}
		})
	}
}

func TestWipeClearsMemoryAndSavedHistory(t *testing.T) {
	m := testTUI(80)
	m.history = historyStore{path: filepath.Join(t.TempDir(), "history")}
	if err := m.history.append("saved"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(m.history.path)
	m.entries = []string{"saved"}
	m.historyAt = 1
	m.acceptResult(tuiEvent("a", "show", "output", 0))
	m.controlOpen = true
	m.controlInput.SetValue(":wipe")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	after, err := os.Stat(m.history.path)
	if err != nil || after.Size() != 0 || !os.SameFile(before, after) || after.Mode().Perm() != 0600 {
		t.Fatal("history file not cleared in place")
	}
	if len(m.entries) != 0 || m.historyAt != 0 || len(m.blocks) != 0 || m.input.Value() != "" {
		t.Fatal("session not wiped")
	}
	if err := m.history.append("new"); err != nil {
		t.Fatal(err)
	}
	got, err := m.history.load()
	if err != nil || !reflect.DeepEqual(got, []string{"new"}) {
		t.Fatal(got, err)
	}
}

func TestWipeFailurePreservesSession(t *testing.T) {
	m := testTUI(80)
	m.history = historyStore{path: t.TempDir()}
	m.entries = []string{"saved"}
	m.acceptResult(tuiEvent("a", "show", "output", 0))
	m.controlOpen = true
	m.controlInput.SetValue(":wipe")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if len(m.entries) != 1 || len(m.blocks) == 0 || m.message == "" {
		t.Fatal("failed wipe discarded session")
	}
}
