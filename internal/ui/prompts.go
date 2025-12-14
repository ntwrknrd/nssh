package ui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/awnumar/memguard"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
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
	var result bool

	confirm := huh.NewConfirm().
		Title(title).
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

// InputHostname shows a text input prompt for a hostname (FQDN, short name, or IP).
// After completion, prints the result so it persists on screen.
func InputHostname(title, defaultValue string) (string, error) {
	result := defaultValue

	input := huh.NewInput().
		Title(title).
		Value(&result).
		Validate(func(s string) error {
			if s == "" {
				return fmt.Errorf("hostname is required")
			}
			return nil
		})

	form := huh.NewForm(huh.NewGroup(input)).
		WithTheme(huhTheme())

	if err := form.Run(); err != nil {
		return "", err
	}

	// Print persisted version after TUI clears
	fmt.Printf("  %s %s : %s\n", DimCyan("[?]"), title, result)

	return result, nil
}

// Password shows a password input prompt (masked).
// After completion, prints a masked indicator so it persists on screen.
func Password(title string) (string, error) {
	var result string

	input := huh.NewInput().
		Title(title).
		EchoMode(huh.EchoModePassword).
		Value(&result)

	form := huh.NewForm(huh.NewGroup(input)).
		WithTheme(huhTheme())

	if err := form.Run(); err != nil {
		return "", err
	}

	// Print persisted version after TUI clears (masked)
	fmt.Printf("  %s %s : %s\n", DimCyan("[?]"), title, strings.Repeat("*", len(result)))

	return result, nil
}

// PasswordWithConfirm shows two password prompts and validates they match.
// Returns an error if passwords don't match or are empty.
func PasswordWithConfirm(title string) (string, error) {
	pw1, err := Password(title)
	if err != nil {
		return "", err
	}
	if pw1 == "" {
		return "", fmt.Errorf("password cannot be empty")
	}

	pw2, err := Password("Confirm password")
	if err != nil {
		return "", err
	}

	if pw1 != pw2 {
		return "", fmt.Errorf("passwords do not match")
	}

	return pw1, nil
}

// InputWithFzf shows a text input prompt with optional Tab-triggered fzf selection.
// User can:
//   - Press Enter to accept default
//   - Type a value and press Enter
//   - Press Tab to launch fzf browser (if choices provided and fzf is available)
//
// After completion, prints the result so it persists on screen.
func InputWithFzf(title, defaultValue string, fzfChoices []string, fzfPrompt string) (string, error) {
	// Fall back to regular input if no choices, not a TTY, or on Windows
	if len(fzfChoices) == 0 || !term.IsTerminal(int(os.Stdin.Fd())) || runtime.GOOS == "windows" {
		if defaultValue != "" {
			return InputWithDefault(title, defaultValue)
		}
		return Input(title, "")
	}

	// If fzf not available, use huh's filtered select as fallback
	if !FzfAvailable() {
		return huhFilteredSelectString(title, fzfChoices)
	}

	fd := int(os.Stdin.Fd())

	// Build prompt text
	var promptText string
	if defaultValue != "" {
		promptText = fmt.Sprintf("  %s %s (%s) [Tab=browse]: ", DimCyan("[?]"), title, defaultValue)
	} else {
		promptText = fmt.Sprintf("  %s %s [Tab=browse]: ", DimCyan("[?]"), title)
	}

	// Print prompt
	fmt.Print(promptText)

	// Save terminal state
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Fall back to regular input if we can't enter raw mode
		fmt.Print("\r\033[K") // Clear line
		if defaultValue != "" {
			return InputWithDefault(title, defaultValue)
		}
		return Input(title, "")
	}

	var buffer strings.Builder
	buf := make([]byte, 1)

	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			if restoreErr := term.Restore(fd, oldState); restoreErr != nil {
				slog.Debug("failed to restore terminal after read error", "err", restoreErr)
			}
			return "", fmt.Errorf("read error: %w", err)
		}

		char := buf[0]

		switch char {
		case 9: // Tab
			// Restore terminal before fzf
			if err := term.Restore(fd, oldState); err != nil {
				slog.Debug("failed to restore terminal before fzf", "err", err)
			}

			// Clear line
			fmt.Print("\r\033[K")

			// Launch fzf
			prompt := fzfPrompt
			if prompt == "" {
				prompt = "Select"
			}
			selected, fzfErr := FuzzySelectString(prompt, fzfChoices)
			if fzfErr != nil {
				// fzf error, fall back to regular input
				return InputWithDefault(title, defaultValue)
			}
			if selected != "" {
				// Print persisted version
				fmt.Printf("  %s %s : %s\n", DimCyan("[?]"), title, selected)
				return selected, nil
			}
			// User canceled fzf, re-show prompt
			fmt.Print(promptText)
			oldState, err = term.MakeRaw(fd)
			if err != nil {
				return InputWithDefault(title, defaultValue)
			}
			buffer.Reset()

		case 13, 10: // Enter (CR or LF)
			_ = term.Restore(fd, oldState)
			fmt.Println()
			result := strings.TrimSpace(buffer.String())
			if result == "" {
				result = defaultValue
			}
			// Print persisted version
			fmt.Printf("  %s %s : %s\n", DimCyan("[?]"), title, result)
			return result, nil

		case 127, 8: // Backspace (DEL or BS)
			if buffer.Len() > 0 {
				// Remove last character from buffer
				s := buffer.String()
				buffer.Reset()
				buffer.WriteString(s[:len(s)-1])
				// Erase character on screen
				fmt.Print("\b \b")
			}

		case 3: // Ctrl+C
			_ = term.Restore(fd, oldState)
			fmt.Println()
			return "", fmt.Errorf("canceled")

		case 4: // Ctrl+D (EOF)
			_ = term.Restore(fd, oldState)
			fmt.Println()
			return "", fmt.Errorf("canceled")

		default:
			// Printable characters (32-126)
			if char >= 32 && char <= 126 {
				buffer.WriteByte(char)
				fmt.Print(string(char))
			}
		}
	}
}

// ErrInterrupted is returned when the user cancels input with Ctrl+C or SIGTERM.
// Callers should check for this error and exit gracefully without printing an error message.
var ErrInterrupted = errors.New("input interrupted")

// PasswordSecure prompts for password input and returns a secure buffer.
// The returned buffer must be destroyed by the caller after use.
// Returns ErrInterrupted if the user cancels with Ctrl+C or SIGTERM.
func PasswordSecure(title string) (*memguard.LockedBuffer, error) {
	// Check if stdin is a terminal
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("password input requires a terminal")
	}

	// Set up signal handler
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Save terminal state for restoration
	oldState, err := term.GetState(fd)
	if err != nil {
		return nil, err
	}

	// Channel for input result
	type result struct {
		buf *memguard.LockedBuffer
		err error
	}
	resultCh := make(chan result, 1)

	go func() {
		// Read password (puts terminal in raw mode internally)
		fmt.Fprintf(os.Stderr, "  %s %s: ", DimCyan("[?]"), title)
		password, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr) // Newline after password input

		if err != nil {
			resultCh <- result{nil, err}
			return
		}

		// Create locked buffer and copy password
		buf := memguard.NewBufferFromBytes(password)
		// Zero the original slice
		for i := range password {
			password[i] = 0
		}

		resultCh <- result{buf, nil}
	}()

	// Wait for either input or signal
	select {
	case <-sigCh:
		// Restore terminal state before returning
		_ = term.Restore(fd, oldState)
		fmt.Fprintln(os.Stderr) // Newline after ^C
		return nil, ErrInterrupted
	case r := <-resultCh:
		return r.buf, r.err
	}
}

// PasswordSecureWithConfirm prompts for password input twice and verifies they match.
// Returns a secure buffer containing the confirmed password.
// The returned buffer must be destroyed by the caller after use.
// Returns ErrInterrupted if the user cancels with Ctrl+C or SIGTERM.
//
// NOTE: This function does NOT validate passphrase requirements (length, etc.).
// For passphrase initialization, use PassphraseStore.Initialize() which validates
// BEFORE confirmation to provide better UX (fail fast on invalid input).
// This function is for general-purpose confirmed input where validation is
// handled by the caller or not required.
func PasswordSecureWithConfirm(title string) (*memguard.LockedBuffer, error) {
	passphraseBuf, err := PasswordSecure(title)
	if err != nil {
		return nil, err
	}

	confirmBuf, err := PasswordSecure("Confirm " + strings.ToLower(title))
	if err != nil {
		passphraseBuf.Destroy()
		return nil, err
	}

	if !bytes.Equal(passphraseBuf.Bytes(), confirmBuf.Bytes()) {
		passphraseBuf.Destroy()
		confirmBuf.Destroy()
		return nil, errors.New("inputs don't match")
	}

	confirmBuf.Destroy()
	return passphraseBuf, nil
}
