package repl

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	core "github.com/ntwrknrd/nssh/internal/repl"
)

func guidedTestModel() model {
	m := testTUI(120)
	m.composer = newComposer()
	m.candidates = []string{"agg1", "border1", "border2"}
	m.refreshLayout()
	return m
}
func composerKey(m model, k tea.KeyMsg) model { next, _ := m.Update(k); return next.(model) }
func TestComposerKeyboardSelectionSurvivesFiltering(t *testing.T) {
	m := guidedTestModel()
	m = composerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("border")})
	m = composerKey(m, tea.KeyMsg{Type: tea.KeySpace})
	m = composerKey(m, tea.KeyMsg{Type: tea.KeySpace})
	m = composerKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = composerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("agg")})
	m = composerKey(m, tea.KeyMsg{Type: tea.KeySpace})
	if !reflect.DeepEqual(m.hosts, []string{"border1", "border2", "agg1"}) {
		t.Fatal(m.hosts)
	}
	m = composerKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.commandFocus {
		t.Fatal("Enter did not open commands")
	}
	m = composerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("show env power")})
	m = composerKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = composerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("show version")})
	s, err := m.guidedSubmission()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Commands, []string{"show env power", "show version"}) {
		t.Fatal(s.Commands)
	}
	if m.active {
		t.Fatal("Enter executed instead of adding a command")
	}
}
func TestComposerPreservesLiteralHostsAndCommandBytes(t *testing.T) {
	m := guidedTestModel()
	m.hosts = []string{"edge(1,2)", "alias,with,commas"}
	commands := []string{`printf 'quoted'`, `echo backslash\`, `echo "$(hostname)"`, "  show padded  "}
	m.commands.SetValue(strings.Join(commands, "\n"))
	s, err := m.guidedSubmission()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Commands, commands) || s.Targets[0].Value != "edge(1,2)" || s.Targets[1].Value != "alias,with,commas" {
		t.Fatalf("changed input: %#v", s)
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	m.hosts = nil
	m.commands.SetValue("")
	m.restoreHistory(guidedHistoryPrefix + string(encoded))
	replay, err := m.guidedSubmission()
	if err != nil || !reflect.DeepEqual(replay, s) {
		t.Fatalf("history=%#v err=%v", replay, err)
	}
	m.restoreHistory("[ 'raw' ] ( 'show' )")
	if m.guided || m.input.Value() != "[ 'raw' ] ( 'show' )" {
		t.Fatal("legacy history not restored")
	}
}
func TestComposerLimitsAndModeDrafts(t *testing.T) {
	m := guidedTestModel()
	if _, err := m.guidedSubmission(); err == nil {
		t.Fatal("allowed no hosts")
	}
	m.hosts = []string{"a"}
	if _, err := m.guidedSubmission(); err == nil {
		t.Fatal("allowed no commands")
	}
	m.commands.SetValue(strings.Repeat("show\n", core.MaxSubmissionCommands+1))
	if _, err := m.guidedSubmission(); err == nil {
		t.Fatal("allowed too many commands")
	}
	m.commands.SetValue("show version")
	m = composerKey(m, tea.KeyMsg{Type: tea.KeyF2})
	if m.guided {
		t.Fatal("F2 did not open syntax editor")
	}
	m = composerKey(m, tea.KeyMsg{Type: tea.KeyF2})
	if !m.guided || m.commands.Value() != "show version" {
		t.Fatal("draft lost switching mode")
	}
	if got := len(strings.Split(m.View(), "\n")); got > m.height {
		t.Fatalf("view height=%d terminal=%d", got, m.height)
	}
}
