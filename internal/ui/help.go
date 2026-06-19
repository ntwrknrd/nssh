package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// StyledHelpConfig holds configuration for styled help rendering.
type StyledHelpConfig struct {
	// ShowGlobalFlags includes global flags in output
	ShowGlobalFlags bool
	// Width is the panel width (0 = auto-detect terminal width)
	Width int
}

// DefaultHelpConfig returns the default styled help configuration.
func DefaultHelpConfig() StyledHelpConfig {
	return StyledHelpConfig{
		ShowGlobalFlags: true,
		Width:           0, // auto-detect
	}
}

// ApplyStyledHelp sets a command to use styled help rendering.
// It also adds an --explain flag for extended help if the command has a Long description.
func ApplyStyledHelp(cmd *cobra.Command) {
	cmd.SetHelpFunc(styledHelpFunc)

	// Add --explain flag if command has extended description
	if cmd.Long != "" {
		addExplainFlag(cmd)
	}
}

// ApplyStyledHelpRecursive applies styled help to a command and all subcommands.
func ApplyStyledHelpRecursive(cmd *cobra.Command) {
	ApplyStyledHelp(cmd)
	for _, sub := range cmd.Commands() {
		ApplyStyledHelpRecursive(sub)
	}
}

// addExplainFlag adds the --explain flag and a PreRunE hook to handle it.
func addExplainFlag(cmd *cobra.Command) {
	cmd.Flags().BoolP("explain", "e", false, "Print command explanation")

	// Chain with existing PreRunE if present
	existingPreRunE := cmd.PreRunE
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		explain, _ := c.Flags().GetBool("explain")
		if explain {
			// Just print the extended description, not the full help
			fmt.Println(strings.TrimSpace(c.Long))
			return errExplainShown
		}

		if existingPreRunE != nil {
			return existingPreRunE(c, args)
		}
		return nil
	}
}

// errExplainShown is returned by PreRunE when --explain was handled.
// The caller should treat this as a successful exit.
var errExplainShown = fmt.Errorf("explain shown")

// IsExplainShown checks if the error is from --explain being handled.
func IsExplainShown(err error) bool {
	return err == errExplainShown
}

// styledHelpFunc is the custom help function for Cobra commands.
func styledHelpFunc(cmd *cobra.Command, args []string) {
	cfg := DefaultHelpConfig()
	output := RenderStyledHelp(cmd, cfg)
	fmt.Println(output)
}

// RenderStyledHelp renders a command's help in styled panel format.
func RenderStyledHelp(cmd *cobra.Command, cfg StyledHelpConfig) string {
	width := cfg.Width
	if width == 0 {
		width = termWidth()
	}
	// Clamp to reasonable bounds
	if width < 40 {
		width = 40
	}
	if width > 80 {
		width = 80
	}

	var sb strings.Builder

	// Usage panel
	sb.WriteString(renderUsagePanel(cmd, width))

	// Commands panel (subcommands)
	if commandsPanel := renderCommandsPanel(cmd, width); commandsPanel != "" {
		sb.WriteString("\n")
		sb.WriteString(commandsPanel)
	}

	// Flags panel (local flags)
	if flagsPanel := renderFlagsPanel(cmd, width); flagsPanel != "" {
		sb.WriteString("\n")
		sb.WriteString(flagsPanel)
	}

	// Global Flags panel (inherited flags)
	if cfg.ShowGlobalFlags {
		if globalPanel := renderGlobalFlagsPanel(cmd, width); globalPanel != "" {
			sb.WriteString("\n")
			sb.WriteString(globalPanel)
		}
	}

	return sb.String()
}

// descriptionColumn is the fixed column where descriptions start (from content edge).
// This ensures alignment between Usage and Flags panels.
const descriptionColumn = 36

// UsageLinesAnnotation lets a command provide explicit usage lines for styled
// help when Cobra's generic "[flags]" suffix obscures the actual command forms.
const UsageLinesAnnotation = "nssh.usage-lines"

// globalFlagNames defines flags that appear in Global Flags (in display order).
var globalFlagNames = []string{"explain", "help", "verbose", "version"}

// globalFlagSet is a set for quick lookup.
var globalFlagSet = func() map[string]bool {
	m := make(map[string]bool)
	for _, name := range globalFlagNames {
		m[name] = true
	}
	return m
}()

// renderUsagePanel renders the Usage section panel.
func renderUsagePanel(cmd *cobra.Command, width int) string {
	usageLines := []string{cmd.UseLine()}
	if cmd.Annotations != nil {
		if annotated := strings.TrimSpace(cmd.Annotations[UsageLinesAnnotation]); annotated != "" {
			usageLines = strings.Split(annotated, "\n")
		}
	}

	content := formatUsageRows(usageLines, cmd.Short, width-6, descriptionColumn)
	return renderPanel("Usage", content, width)
}

// renderCommandsPanel renders the Commands panel with subcommands.
func renderCommandsPanel(cmd *cobra.Command, width int) string {
	subcommands := cmd.Commands()

	// Filter out hidden commands
	var visible []*cobra.Command
	for _, sub := range subcommands {
		if !sub.Hidden {
			visible = append(visible, sub)
		}
	}

	if len(visible) == 0 {
		return ""
	}

	content := formatCommands(visible, width-6, descriptionColumn)
	return renderPanel("Commands", content, width)
}

// formatCommands formats subcommands into aligned rows.
func formatCommands(commands []*cobra.Command, width, descCol int) string {
	var lines []string

	cmdStyle := lipgloss.NewStyle().Foreground(ColorWhite)
	descStyle := lipgloss.NewStyle().Foreground(ColorGray)

	// Command column width is descCol - 2 (for leading "  ")
	cmdColWidth := descCol - 2
	descWidth := width - descCol

	for _, cmd := range commands {
		name := cmd.Name()
		short := cmd.Short

		// Truncate if needed
		if len(name) > cmdColWidth-1 {
			name = name[:cmdColWidth-4] + "..."
		}
		if len(short) > descWidth {
			short = short[:descWidth-3] + "..."
		}

		line := fmt.Sprintf("  %s%s",
			cmdStyle.Render(padRight(name, cmdColWidth)),
			descStyle.Render(short))
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// renderFlagsPanel renders the Flags panel with local flags only.
func renderFlagsPanel(cmd *cobra.Command, width int) string {
	localFlags := cmd.LocalFlags()

	// Filter out global flags.
	var flags []*pflag.Flag
	localFlags.VisitAll(func(f *pflag.Flag) {
		if !globalFlagSet[f.Name] {
			flags = append(flags, f)
		}
	})

	if len(flags) == 0 {
		return ""
	}

	content := formatFlags(flags, width-6, descriptionColumn)
	return renderPanel("Flags", content, width)
}

// renderGlobalFlagsPanel renders the Global Flags panel with inherited flags.
func renderGlobalFlagsPanel(cmd *cobra.Command, width int) string {
	var flags []*pflag.Flag

	// Add global flags first (in defined order, if present).
	for _, name := range globalFlagNames {
		if f := cmd.Flags().Lookup(name); f != nil {
			flags = append(flags, f)
		}
	}

	// Add inherited flags, excluding global flags already added.
	cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) {
		if !globalFlagSet[f.Name] {
			flags = append(flags, f)
		}
	})

	if len(flags) == 0 {
		return ""
	}

	content := formatFlags(flags, width-6, descriptionColumn)
	return renderPanel("Global Flags", content, width)
}

// renderPanel renders content inside a styled panel box.
func renderPanel(title, content string, width int) string {
	return BorderedPanel(title, content, "left", width)
}

// formatUsageRow formats a usage line with command and description.
func formatUsageRow(usage, description string, width, descCol int) string {
	// descCol is where descriptions start (from content edge)
	cmdCol := descCol - 2 // Account for leading "  "
	fullCmdCol := width - 2
	usageStyle := lipgloss.NewStyle().Foreground(ColorWhite)
	descStyle := lipgloss.NewStyle().Foreground(ColorGray)

	if len(usage) > cmdCol-1 && len(usage) <= fullCmdCol {
		return fmt.Sprintf("  %s", usageStyle.Render(padRight(usage, fullCmdCol)))
	}

	// Truncate command if needed
	if len(usage) > cmdCol-1 {
		usage = usage[:cmdCol-4] + "..."
	}

	// Calculate available space for description
	descWidth := width - descCol
	if len(description) > descWidth {
		description = description[:descWidth-3] + "..."
	}

	return fmt.Sprintf("  %s%s",
		usageStyle.Render(padRight(usage, cmdCol)),
		descStyle.Render(description))
}

func formatUsageRows(usages []string, description string, width, descCol int) string {
	lines := make([]string, 0, len(usages))
	for i, usage := range usages {
		desc := ""
		if i == 0 {
			desc = description
		}
		lines = append(lines, formatUsageRow(strings.TrimSpace(usage), desc, width, descCol))
	}
	return strings.Join(lines, "\n")
}

// formatFlags formats a slice of flags into aligned rows.
func formatFlags(flags []*pflag.Flag, width, descCol int) string {
	if len(flags) == 0 {
		return ""
	}

	// Format each flag using consistent description column
	var lines []string
	for _, f := range flags {
		line := formatFlagRow(f, width, descCol)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// formatFlagName formats a flag's name portion (e.g., "-s, --ssh-config STRING").
func formatFlagName(f *pflag.Flag) string {
	var parts []string

	// Short flag first (Python style)
	if f.Shorthand != "" {
		parts = append(parts, "-"+f.Shorthand)
	}

	// Long flag
	longFlag := "--" + f.Name

	// Type indicator (uppercase for visibility)
	typeStr := flagTypeString(f)
	if typeStr != "" {
		longFlag += " " + typeStr
	}

	parts = append(parts, longFlag)

	return strings.Join(parts, ", ")
}

// flagTypeString returns the uppercase type indicator for a flag.
func flagTypeString(f *pflag.Flag) string {
	// Don't show type for bool flags
	if f.Value.Type() == "bool" {
		return ""
	}

	typ := strings.ToUpper(f.Value.Type())

	// Normalize common types
	switch typ {
	case "STRING":
		return "STRING"
	case "INT", "INT32", "INT64":
		return "INT"
	case "FLOAT32", "FLOAT64":
		return "FLOAT"
	case "DURATION":
		return "DURATION"
	case "STRINGSLICE":
		return "STRINGS"
	default:
		return typ
	}
}

// formatFlagRow formats a single flag row with alignment.
func formatFlagRow(f *pflag.Flag, totalWidth, descCol int) string {
	flagStr := formatFlagName(f)
	usage := f.Usage

	// Style the flag name
	flagStyle := lipgloss.NewStyle().Foreground(ColorWhite)

	// Style the description
	descStyle := lipgloss.NewStyle().Foreground(ColorGray)

	// Flag column width is descCol - 2 (for leading "  ")
	flagColWidth := descCol - 2

	// Truncate flag name if needed
	if len(flagStr) > flagColWidth-1 {
		flagStr = flagStr[:flagColWidth-4] + "..."
	}

	// Calculate available space for description
	descWidth := totalWidth - descCol
	if descWidth < 10 {
		descWidth = 10
	}

	// Truncate description if needed
	if len(usage) > descWidth {
		usage = usage[:descWidth-3] + "..."
	}

	return fmt.Sprintf("  %s%s",
		flagStyle.Render(padRight(flagStr, flagColWidth)),
		descStyle.Render(usage))
}

// padRight pads a string to the specified width with spaces.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
