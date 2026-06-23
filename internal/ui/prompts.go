package ui

import (
	"errors"
	"fmt"
	"io"

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
