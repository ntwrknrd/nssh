package ui

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/huh"
)

// FuzzySelectOption represents an option in fuzzy selection.
type FuzzySelectOption struct {
	// Label is the main display text
	Label string

	// Description is the secondary text (shown on the right)
	Description string

	// Value is the underlying value to return when selected
	Value any
}

// FzfAvailable checks if fzf is installed.
func FzfAvailable() bool {
	_, err := exec.LookPath("fzf")
	return err == nil
}

// huhFilteredSelect provides a fallback selection UI when fzf is unavailable.
func huhFilteredSelect(title string, options []huh.Option[int]) (int, error) {
	var selected = -1

	sel := huh.NewSelect[int]().
		Title(title).
		Options(options...).
		Height(10).
		Value(&selected)

	form := huh.NewForm(huh.NewGroup(sel)).
		WithTheme(huhTheme())

	if err := form.Run(); err != nil {
		return -1, err
	}

	// Print persisted result (find label for selected index)
	for _, opt := range options {
		if opt.Value == selected {
			fmt.Printf("  %s %s : %s\n", DimCyan("[?]"), title, opt.Key)
			break
		}
	}

	return selected, nil
}

// huhFilteredSelectString provides a fallback selection UI for string options.
func huhFilteredSelectString(title string, options []string) (string, error) {
	var selected string

	huhOpts := make([]huh.Option[string], len(options))
	for i, opt := range options {
		huhOpts[i] = huh.NewOption(opt, opt)
	}

	sel := huh.NewSelect[string]().
		Title(title).
		Options(huhOpts...).
		Height(10).
		Value(&selected)

	form := huh.NewForm(huh.NewGroup(sel)).
		WithTheme(huhTheme())

	if err := form.Run(); err != nil {
		return "", err
	}

	// Print persisted result
	fmt.Printf("  %s %s : %s\n", DimCyan("[?]"), title, selected)

	return selected, nil
}

// fzfSelect invokes fzf with the given options and returns the selected string.
func fzfSelect(prompt string, options []string, multi bool, initialQuery string) ([]string, error) {
	args := []string{
		"--layout=reverse",
		"--height=40%",
		"--prompt", prompt + " > ",
	}

	if multi {
		args = append(args, "--multi")
	}

	if initialQuery != "" {
		args = append(args, "--query", initialQuery)
	}

	cmd := exec.Command("fzf", args...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("fzf stdin pipe: %w", err)
	}

	go func() {
		defer func() {
			if err := stdin.Close(); err != nil {
				slog.Debug("failed to close fzf stdin", "err", err)
			}
		}()
		for _, opt := range options {
			if _, err := fmt.Fprintln(stdin, opt); err != nil {
				slog.Debug("failed to write to fzf stdin", "err", err)
				return
			}
		}
	}()

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 1 = no match, 2 = error, 130 = interrupted (Ctrl+C)
			if exitErr.ExitCode() == 1 || exitErr.ExitCode() == 130 {
				return nil, nil // User canceled or no match
			}
		}
		return nil, fmt.Errorf("fzf: %w", err)
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return nil, nil
	}

	// Split by newlines for multi-select
	lines := strings.Split(result, "\n")
	var selected []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			selected = append(selected, line)
		}
	}

	return selected, nil
}

// FuzzySelect presents a fuzzy finder interface and returns the selected option.
// Returns the index of the selected option, or -1 if canceled.
// Optional initialQuery pre-fills the search input.
// Falls back to huh's filtered select if fzf is unavailable.
func FuzzySelect(prompt string, options []FuzzySelectOption, initialQuery ...string) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("no options provided")
	}

	// Extract labels for fzf
	labels := make([]string, len(options))
	for i, opt := range options {
		labels[i] = opt.Label
	}

	// Try fzf first
	if FzfAvailable() {
		query := ""
		if len(initialQuery) > 0 {
			query = initialQuery[0]
		}

		selected, err := fzfSelect(prompt, labels, false, query)
		if err != nil {
			return -1, err
		}
		if len(selected) == 0 {
			return -1, nil // Canceled
		}

		// Find index of selected label
		for i, opt := range options {
			if opt.Label == selected[0] {
				return i, nil
			}
		}

		return -1, fmt.Errorf("selected option not found")
	}

	// Fallback to huh's filtered select
	huhOpts := make([]huh.Option[int], len(options))
	for i, opt := range options {
		huhOpts[i] = huh.NewOption(opt.Label, i)
	}

	return huhFilteredSelect(prompt, huhOpts)
}

// FuzzySelectString presents a fuzzy finder for a simple string list.
// Returns the selected string, or empty string if canceled.
// Optional initialQuery pre-fills the search input.
// Falls back to huh's filtered select if fzf is unavailable.
func FuzzySelectString(prompt string, options []string, initialQuery ...string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
	}

	// Try fzf first
	if FzfAvailable() {
		query := ""
		if len(initialQuery) > 0 {
			query = initialQuery[0]
		}

		selected, err := fzfSelect(prompt, options, false, query)
		if err != nil {
			return "", err
		}
		if len(selected) == 0 {
			return "", nil // Canceled
		}

		return selected[0], nil
	}

	// Fallback to huh's filtered select
	return huhFilteredSelectString(prompt, options)
}

// FuzzySelectMulti presents a fuzzy finder for multi-selection.
// Returns indices of selected options, or empty slice if canceled.
// Falls back to huh's multi-select if fzf is unavailable.
func FuzzySelectMulti(prompt string, options []FuzzySelectOption) ([]int, error) {
	if len(options) == 0 {
		return nil, fmt.Errorf("no options provided")
	}

	// Extract labels for fzf
	labels := make([]string, len(options))
	for i, opt := range options {
		labels[i] = opt.Label
	}

	// Try fzf first
	if FzfAvailable() {
		selected, err := fzfSelect(prompt, labels, true, "")
		if err != nil {
			return nil, err
		}
		if len(selected) == 0 {
			return nil, nil // Canceled
		}

		// Find indices of selected labels
		labelToIdx := make(map[string]int)
		for i, opt := range options {
			labelToIdx[opt.Label] = i
		}

		var indices []int
		for _, sel := range selected {
			if idx, ok := labelToIdx[sel]; ok {
				indices = append(indices, idx)
			}
		}

		return indices, nil
	}

	// Fallback to huh's multi-select
	huhOpts := make([]huh.Option[int], len(options))
	for i, opt := range options {
		huhOpts[i] = huh.NewOption(opt.Label, i)
	}

	var selected []int
	sel := huh.NewMultiSelect[int]().
		Title(prompt).
		Options(huhOpts...).
		Height(10).
		Value(&selected)

	form := huh.NewForm(huh.NewGroup(sel)).
		WithTheme(huhTheme())

	if err := form.Run(); err != nil {
		return nil, err
	}

	// Print persisted result
	if len(selected) > 0 {
		var labels []string
		for _, idx := range selected {
			labels = append(labels, options[idx].Label)
		}
		fmt.Printf("  %s %s : %s\n", DimCyan("[?]"), prompt, strings.Join(labels, ", "))
	}

	return selected, nil
}

// HostSelectOption creates a FuzzySelectOption for a host entry.
func HostSelectOption(alias, hostname, user, configFile string) FuzzySelectOption {
	desc := fmt.Sprintf("Hostname: %s\nUser: %s\nConfig: %s", hostname, user, configFile)
	return FuzzySelectOption{
		Label:       alias,
		Description: desc,
		Value:       alias,
	}
}
