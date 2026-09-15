package repl

import (
	"encoding/json"
	"reflect"
	"testing"

	core "github.com/ntwrknrd/nssh/internal/repl"
)

func TestRestoreFormHistoryAsEditableSyntax(t *testing.T) {
	s := core.Submission{Targets: []core.Target{{Value: "edge1"}}, Commands: []string{"show version", "printf 'quoted'"}}
	data, _ := json.Marshal(s)
	m := testTUI(120)
	m.restoreHistory(guidedHistoryPrefix + string(data))
	got, err := core.Parse(m.input.Value())
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("%#v %v %s", got, err, m.message)
	}
}
func TestRestoreFormHistoryRejectsChangedMeaning(t *testing.T) {
	s := core.Submission{Targets: []core.Target{{Value: "edge(1,2)"}}, Commands: []string{"show"}}
	data, _ := json.Marshal(s)
	m := testTUI(120)
	m.input.SetValue("draft")
	m.restoreHistory(guidedHistoryPrefix + string(data))
	if m.input.Value() != "draft" || m.message == "" {
		t.Fatal("changed meaning")
	}
}
