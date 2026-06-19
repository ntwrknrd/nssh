package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ReviewText shows scrollable text and returns whether the user approved it.
func ReviewText(title, body string, defaultValue bool) (bool, error) {
	if strings.TrimSpace(body) == "" {
		return Confirm(title, defaultValue)
	}
	model := newReviewModel(title, body, defaultValue)
	final, err := tea.NewProgram(model).Run()
	if err != nil {
		return false, err
	}
	result, ok := final.(reviewModel)
	if !ok {
		return false, nil
	}
	return result.approved, nil
}

type reviewModel struct {
	title        string
	body         string
	defaultValue bool
	approved     bool
	viewport     viewport.Model
}

func newReviewModel(title, body string, defaultValue bool) reviewModel {
	vp := viewport.New(80, 20)
	vp.SetHorizontalStep(4)
	vp.SetContent(styleReviewText(body))
	return reviewModel{
		title:        title,
		body:         body,
		defaultValue: defaultValue,
		viewport:     vp,
	}
}

func (m reviewModel) Init() tea.Cmd {
	return nil
}

func (m reviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewport.Width = max(msg.Width, 20)
		m.viewport.Height = max(msg.Height-4, 5)
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			m.approved = true
			return m, tea.Quit
		case "n", "N", "esc", "q", "ctrl+c":
			m.approved = false
			return m, tea.Quit
		case "enter":
			m.approved = m.defaultValue
			return m, tea.Quit
		case "g":
			m.viewport.GotoTop()
			return m, nil
		case "G":
			m.viewport.GotoBottom()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m reviewModel) View() string {
	title := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(m.title)
	position := lipgloss.NewStyle().Foreground(ColorGray).Render(
		fmtPercent(m.viewport.ScrollPercent()) + "  j/k scroll  pgup/pgdn page  g/G top/bottom  y apply  n cancel",
	)
	return title + "\n" + m.viewport.View() + "\n" + position
}

func styleReviewText(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			lines[i] = lipgloss.NewStyle().Foreground(ColorWhite).Bold(true).Render(line)
		case strings.HasPrefix(line, "@@"):
			lines[i] = lipgloss.NewStyle().Foreground(ColorCyan).Render(line)
		case strings.HasPrefix(line, "+"):
			lines[i] = lipgloss.NewStyle().Foreground(ColorGreen).Render(line)
		case strings.HasPrefix(line, "-"):
			lines[i] = lipgloss.NewStyle().Foreground(ColorRed).Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

func fmtPercent(value float64) string {
	percent := int(value*100 + 0.5)
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return lipgloss.NewStyle().Foreground(ColorGray).Render("scroll " + strconv.Itoa(percent) + "%")
}
