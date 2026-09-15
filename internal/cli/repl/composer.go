package repl

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"

	core "github.com/ntwrknrd/nssh/internal/repl"
)

const guidedHistoryPrefix = "@guided "

// Old form history must become editable syntax without changing what it runs.
func (m *model) restoreHistory(entry string) {
	if strings.HasPrefix(entry, guidedHistoryPrefix) {
		var s core.Submission
		if json.Unmarshal([]byte(strings.TrimPrefix(entry, guidedHistoryPrefix)), &s) != nil {
			m.message = "Cannot restore invalid form history"
			return
		}
		quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "\\'") + "'" }
		var hosts, commands []string
		for _, t := range s.Targets {
			hosts = append(hosts, quote(t.Value))
		}
		for _, c := range s.Commands {
			commands = append(commands, quote(c))
		}
		entry = "[ " + strings.Join(hosts, ", ") + " ] ( " + strings.Join(commands, ", ") + " )"
		parsed, err := core.Parse(entry)
		if err != nil || !reflect.DeepEqual(parsed, s) {
			m.message = "Cannot safely convert this form history entry to syntax"
			return
		}
	}
	m.input.SetValue(entry)
	m.input.CursorEnd()
	m.message = ""
}
func (m *model) startSubmission(submission core.Submission, history string) {
	m.matches = nil
	m.pickerOpen = false
	m.message = ""
	m.active = true
	ctx, cancel := context.WithCancel(m.owner.ctx)
	m.cancel = cancel
	m.entries = boundedHistory(append(m.entries, history))
	m.historyAt = len(m.entries)
	if err := m.history.append(history); err != nil {
		m.appendTranscript("history: " + err.Error() + "\n")
	}
	m.owner.workers.Add(1)
	go func() {
		defer m.owner.workers.Done()
		defer cancel()
		err := runSubmission(ctx, submission, m.concurrency, io.Discard, io.Discard, m.owner)
		m.owner.program.Send(finishedMsg{err: err})
	}()
}
