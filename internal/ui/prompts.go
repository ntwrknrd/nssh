package ui

import (
	"errors"
	"fmt"
	"io"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// huhTheme returns a custom theme matching our color palette.
// Adds 2-space left margin to match ui.Info/ui.Success indent.
func huhTheme() *huh.Theme {
	t := huh.ThemeBase()

	// Add left margin to Base styles (preserves existing styling, adds margin)
	t.Focused.Base = t.Focused.Base.MarginLeft(2)
	t.Blurred.Base = t.Blurred.Base.MarginLeft(2)

	// Customize focused styles
	t.Focused.Title = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	t.Focused.Description = lipgloss.NewStyle().Foreground(ColorGray)
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(ColorGreen).SetString("> ")
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(ColorGreen)
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(ColorWhite)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(ColorGreen)
	t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(ColorWhite)

	// Blurred styles (less prominent)
	t.Blurred.Title = lipgloss.NewStyle().Foreground(ColorGray)
	t.Blurred.SelectSelector = lipgloss.NewStyle().Foreground(ColorDim).SetString("  ")

	return t
}

// SelectOption represents a choice in a select prompt.
type SelectOption struct {
	Label string
	Value string
}

// Select shows a select prompt and returns the selected value.
// Returns empty string if canceled.
func Select(title string, options []SelectOption) (string, error) {
	if len(options) == 0 {
		return "", nil
	}

	var selected string

	// Build huh options
	huhOpts := make([]huh.Option[string], len(options))
	for i, opt := range options {
		val := opt.Value
		if val == "" {
			val = opt.Label
		}
		huhOpts[i] = huh.NewOption(opt.Label, val)
	}

	sel := huh.NewSelect[string]().
		Title(title).
		Options(huhOpts...).
		Value(&selected)

	form := huh.NewForm(huh.NewGroup(sel)).
		WithTheme(huhTheme())

	if err := form.Run(); err != nil {
		return "", err
	}

	return selected, nil
}

// SelectMulti shows a multi-select prompt and returns selected values.
// Returns an empty slice if nothing is selected.
func SelectMulti(title string, options []SelectOption) ([]string, error) {
	if len(options) == 0 {
		return nil, nil
	}

	fuzzyOptions := make([]FuzzySelectOption, len(options))
	for i, opt := range options {
		val := opt.Value
		if val == "" {
			val = opt.Label
		}
		fuzzyOptions[i] = FuzzySelectOption{
			Label: opt.Label,
			Value: val,
		}
	}

	indices, err := FuzzySelectMulti(title, fuzzyOptions)
	if err != nil {
		return nil, err
	}
	selected := make([]string, 0, len(indices))
	for _, idx := range indices {
		if idx < 0 || idx >= len(fuzzyOptions) {
			continue
		}
		if value, ok := fuzzyOptions[idx].Value.(string); ok {
			selected = append(selected, value)
		}
	}
	return selected, nil
}

// SelectIndex shows a select prompt and returns the selected index.
// Returns -1 if canceled.
func SelectIndex(title string, options []string, input io.Reader) (int, error) {
	if len(options) == 0 {
		return -1, nil
	}

	var selected int

	// Build huh options with index as value
	huhOpts := make([]huh.Option[int], len(options))
	for i, opt := range options {
		huhOpts[i] = huh.NewOption(opt, i)
	}

	sel := huh.NewSelect[int]().
		Title(title).
		Options(huhOpts...).
		Value(&selected)

	form := huh.NewForm(huh.NewGroup(sel)).
		WithTheme(huhTheme())

	if input != nil {
		form.WithInput(input)
	}

	if err := form.Run(); err != nil {
		return -1, err
	}

	return selected, nil
}

// SelectIndexWithShortcuts shows a selection list whose options can also be
// chosen with a single key. Enter selects the highlighted option, which starts
// on the first one. Filtering is disabled so letter keys stay shortcuts.
func SelectIndexWithShortcuts(title string, options []string, shortcuts map[rune]int, input io.Reader) (int, error) {
	if len(options) == 0 {
		return -1, nil
	}

	var selected int
	huhOpts := make([]huh.Option[int], len(options))
	for i, opt := range options {
		huhOpts[i] = huh.NewOption(opt, i)
	}
	sel := huh.NewSelect[int]().
		Title(title).
		Options(huhOpts...).
		Filtering(false).
		Value(&selected)
	form := huh.NewForm(huh.NewGroup(sel)).WithTheme(huhTheme())
	form.SubmitCmd = tea.Quit
	form.CancelCmd = tea.Interrupt

	model := &shortcutSelect{form: form, shortcuts: shortcuts, chosen: -1}
	teaOpts := []tea.ProgramOption{}
	if input != nil {
		teaOpts = append(teaOpts, tea.WithInput(input))
	}
	if _, err := tea.NewProgram(model, teaOpts...).Run(); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return -1, huh.ErrUserAborted
		}
		return -1, err
	}
	if model.chosen >= 0 {
		return model.chosen, nil
	}
	if form.State != huh.StateCompleted {
		return -1, huh.ErrUserAborted
	}
	return selected, nil
}

type shortcutSelect struct {
	form      *huh.Form
	shortcuts map[rune]int
	chosen    int
}

func (m *shortcutSelect) Init() tea.Cmd { return m.form.Init() }

func (m *shortcutSelect) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyRunes && len(key.Runes) == 1 {
		if idx, ok := m.shortcuts[unicode.ToLower(key.Runes[0])]; ok {
			m.chosen = idx
			return m, tea.Quit
		}
	}
	model, cmd := m.form.Update(msg)
	if form, ok := model.(*huh.Form); ok {
		m.form = form
	}
	return m, cmd
}

func (m *shortcutSelect) View() string {
	if m.chosen >= 0 || m.form.State != huh.StateNormal {
		return ""
	}
	return m.form.View()
}

// Confirm shows a yes/no confirmation prompt.
// Returns the user's choice.
func Confirm(title string, defaultValue bool) (bool, error) {
	return ConfirmWithDescription(title, "", defaultValue)
}

// IsUserAbort reports whether an interactive prompt was canceled by the user.
func IsUserAbort(err error) bool {
	return errors.Is(err, huh.ErrUserAborted)
}

// ConfirmWithDescription shows a yes/no confirmation prompt with optional detail.
// Returns the user's choice.
func ConfirmWithDescription(title, description string, defaultValue bool) (bool, error) {
	var result bool

	confirm := huh.NewConfirm().
		Title(title).
		Description(description).
		Affirmative("Yes").
		Negative("No").
		Value(&result)

	// Set default
	if defaultValue {
		result = true
	}

	form := huh.NewForm(huh.NewGroup(confirm)).
		WithTheme(huhTheme())

	if err := form.Run(); err != nil {
		return false, err
	}

	return result, nil
}

// Input shows a text input prompt.
// After completion, prints the result so it persists on screen.
func Input(title, placeholder string) (string, error) {
	var result string

	input := huh.NewInput().
		Title(title).
		Placeholder(placeholder).
		Value(&result)

	form := huh.NewForm(huh.NewGroup(input)).
		WithTheme(huhTheme())

	if err := form.Run(); err != nil {
		return "", err
	}

	// Print persisted version after TUI clears
	fmt.Printf("  %s %s : %s\n", DimCyan("[?]"), title, result)

	return result, nil
}

// InputWithDefault shows a text input prompt with a pre-filled default value.
// After completion, prints the result so it persists on screen.
func InputWithDefault(title, defaultValue string) (string, error) {
	result := defaultValue

	input := huh.NewInput().
		Title(title).
		Value(&result)

	form := huh.NewForm(huh.NewGroup(input)).
		WithTheme(huhTheme())

	if err := form.Run(); err != nil {
		return "", err
	}

	// Print persisted version after TUI clears
	fmt.Printf("  %s %s : %s\n", DimCyan("[?]"), title, result)

	return result, nil
}

// InputWithDefaultSilent shows a text input prompt with a pre-filled default
// value without printing a persisted summary after the TUI clears.
func InputWithDefaultSilent(title, defaultValue string) (string, error) {
	result := defaultValue

	input := huh.NewInput().
		Title(title).
		Value(&result)

	form := huh.NewForm(huh.NewGroup(input)).
		WithTheme(huhTheme())

	if err := form.Run(); err != nil {
		return "", err
	}

	return result, nil
}

// Password shows a masked password input without printing the entered value.
func Password(title string) (string, error) {
	var result string
	input := huh.NewInput().
		Title(title).
		EchoMode(huh.EchoModePassword).
		Value(&result)
	form := huh.NewForm(huh.NewGroup(input)).WithTheme(huhTheme())
	if err := form.Run(); err != nil {
		return "", err
	}
	return result, nil
}
