package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	core "github.com/ntwrknrd/nssh/internal/repl"
)

type composer struct {
	guided, commandFocus bool
	hostFilter           textinput.Model
	commands             textarea.Model
	hosts                []string
	hostAt               int
}

func newComposer() composer {
	filter := textinput.New()
	filter.Prompt = "Filter: "
	filter.Placeholder = "type to search inventory"
	filter.CharLimit = 256
	filter.Focus()
	commands := textarea.New()
	commands.Prompt = ""
	commands.Placeholder = "show env power\nshow version"
	commands.CharLimit = core.MaxSubmissionBytes
	commands.SetHeight(3)
	return composer{guided: true, hostFilter: filter, commands: commands}
}
func (m model) filteredHosts() []string {
	query := strings.ToLower(strings.TrimSpace(m.hostFilter.Value()))
	var hosts []string
	for _, host := range m.candidates {
		if strings.Contains(strings.ToLower(host), query) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}
func (m model) hasHost(host string) bool {
	for _, selected := range m.hosts {
		if selected == host {
			return true
		}
	}
	return false
}
func (m *model) toggleHost(host string) {
	for i, selected := range m.hosts {
		if selected == host {
			m.hosts = append(m.hosts[:i], m.hosts[i+1:]...)
			return
		}
	}
	if len(m.hosts) >= core.MaxSubmissionTargets {
		m.message = "host limit reached"
		return
	}
	m.hosts = append(m.hosts, host)
}
func (m *model) focusCommands(on bool) tea.Cmd {
	m.commandFocus = on
	if on {
		m.hostFilter.Blur()
		return m.commands.Focus()
	}
	m.commands.Blur()
	return m.hostFilter.Focus()
}
func (m model) guidedSubmission() (core.Submission, error) {
	var s core.Submission
	if len(m.hosts) == 0 {
		return s, fmt.Errorf("select at least one host")
	}
	size := 0
	for _, host := range m.hosts {
		s.Targets = append(s.Targets, core.Target{Value: host})
		size += len(host)
	}
	for _, line := range strings.Split(strings.ReplaceAll(m.commands.Value(), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		s.Commands = append(s.Commands, line)
		size += len(line)
	}
	if len(s.Commands) == 0 {
		return s, fmt.Errorf("enter at least one command")
	}
	if len(s.Commands) > core.MaxSubmissionCommands || len(s.Targets) > core.MaxSubmissionTargets || len(s.Commands)*len(s.Targets) > core.MaxSubmissionJobs || size > core.MaxSubmissionBytes {
		return s, fmt.Errorf("submission exceeds REPL limits")
	}
	return s, nil
}
func (m model) updateComposer(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch key.Type {
	case tea.KeyF5:
		s, err := m.guidedSubmission()
		if err != nil {
			m.message = err.Error()
			return m, nil
		}
		encoded, err := json.Marshal(s)
		if err != nil {
			m.message = err.Error()
			return m, nil
		}
		m.startSubmission(s, guidedHistoryPrefix+string(encoded))
		return m, nil
	case tea.KeyTab, tea.KeyShiftTab:
		cmd = m.focusCommands(!m.commandFocus)
	case tea.KeyEsc:
		if m.commandFocus {
			cmd = m.focusCommands(false)
		} else {
			m.hostFilter.SetValue("")
			m.hostAt = 0
		}
	case tea.KeyPgUp, tea.KeyPgDown:
		m.viewport, cmd = m.viewport.Update(key)
	case tea.KeyCtrlP, tea.KeyCtrlN:
		if len(m.entries) > 0 {
			if key.Type == tea.KeyCtrlP {
				m.historyAt = max(0, m.historyAt-1)
			} else {
				m.historyAt = min(len(m.entries)-1, m.historyAt+1)
			}
			m.restoreHistory(m.entries[m.historyAt])
		}
	default:
		if m.commandFocus {
			m.commands, cmd = m.commands.Update(key)
		} else {
			hosts := m.filteredHosts()
			m.hostAt = min(max(0, m.hostAt), max(0, len(hosts)-1))
			switch key.Type {
			case tea.KeyUp:
				m.hostAt = max(0, m.hostAt-1)
			case tea.KeyDown:
				m.hostAt = min(max(0, len(hosts)-1), m.hostAt+1)
			case tea.KeySpace:
				if len(hosts) > 0 {
					m.toggleHost(hosts[m.hostAt])
					m.hostAt = min(len(hosts)-1, m.hostAt+1)
				}
			case tea.KeyEnter:
				if len(m.hosts) == 0 && len(hosts) > 0 {
					m.toggleHost(hosts[m.hostAt])
				}
				if len(m.hosts) > 0 {
					cmd = m.focusCommands(true)
				} else {
					m.message = "no hosts selected"
				}
			case tea.KeyCtrlA:
				for _, host := range hosts {
					if !m.hasHost(host) {
						m.toggleHost(host)
					}
				}
			case tea.KeyCtrlX:
				m.hosts = nil
			default:
				m.hostFilter, cmd = m.hostFilter.Update(key)
				m.hostAt = 0
			}
		}
	}
	m.refreshLayout()
	return m, cmd
}
func (m model) composerView(width int) string {
	hosts := m.filteredHosts()
	var rows []string
	label := "Hosts"
	if !m.commandFocus {
		label = "> Hosts"
	}
	rows = append(rows, tuiTarget.Bold(true).Render(fmt.Sprintf("%s (%d selected, %d matches)", label, len(m.hosts), len(hosts))))
	if !m.commandFocus {
		rows = append(rows, m.hostFilter.View())
		first := max(0, m.hostAt-3)
		if len(hosts) == 0 {
			rows = append(rows, "  No matching inventory hosts. F2 opens literal/selector syntax.")
		}
		for i := first; i < min(len(hosts), first+4); i++ {
			marker := "[ ]"
			if m.hasHost(hosts[i]) {
				marker = "[x]"
			}
			cursor := "  "
			if i == m.hostAt {
				cursor = "> "
			}
			rows = append(rows, cursor+marker+" "+displayLabel(hosts[i]))
		}
		rows = append(rows, tuiDim.Render("Space select  Enter commands  Ctrl-A select matches  Ctrl-X clear"))
	}
	if len(m.hosts) > 0 {
		rows = append(rows, "Selected: "+displayLabel(strings.Join(m.hosts, ", ")))
	}
	label = "Commands (one per line)"
	if m.commandFocus {
		label = "> " + label
	}
	rows = append(rows, tuiCommand.Bold(true).Render(label))
	// Keep the native editor's cursor and scrolling; users type command text only.
	editor := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8")).Width(max(1, width-2)).Render(m.commands.View())
	rows = append(rows, editor, tuiDim.Render("Enter new command  F5 run  Tab switch  Ctrl-P/N history"))
	for i, row := range rows {
		if !strings.Contains(row, "\n") {
			rows[i] = ansi.Truncate(row, max(1, width), "...")
		}
	}
	return strings.Join(rows, "\n")
}

const guidedHistoryPrefix = "@guided "

func (m *model) restoreHistory(entry string) {
	if strings.HasPrefix(entry, guidedHistoryPrefix) {
		var s core.Submission
		if json.Unmarshal([]byte(strings.TrimPrefix(entry, guidedHistoryPrefix)), &s) == nil {
			m.guided = true
			m.hosts = nil
			for _, target := range s.Targets {
				m.hosts = append(m.hosts, target.Value)
			}
			m.commands.SetValue(strings.Join(s.Commands, "\n"))
			m.focusCommands(true)
			m.refreshLayout()
			return
		}
	}
	m.guided = false
	m.input.SetValue(entry)
}
func (m *model) startSubmission(submission core.Submission, history string) {
	m.matches = nil
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
