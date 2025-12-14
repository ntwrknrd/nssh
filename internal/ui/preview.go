package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// InsertionPreview displays a diff-style preview of where a new host
// will be inserted in the SSH config file.
//
// Parameters:
//   - newLines: The new host config lines to be inserted
//   - beforeHost: Lines of the host that will appear before (nil if inserting at start)
//   - afterHost: Lines of the host that will appear after (nil if inserting at end)
//   - targetFile: Path to the target file (for display)
//   - maxContextLines: Maximum lines to show per context block (0 = show all)
func InsertionPreview(newLines, beforeHost, afterHost []string, targetFile string, maxContextLines int) {
	if maxContextLines == 0 {
		maxContextLines = 6
	}

	// Styles
	dimStyle := lipgloss.NewStyle().Foreground(ColorDim)
	addStyle := lipgloss.NewStyle().Foreground(ColorGreen)
	borderStyle := lipgloss.NewStyle().Foreground(ColorDim)

	width := termWidth()
	innerWidth := width - 4 // Account for "| " prefix and " |" suffix

	// Top border
	topBorder := borderStyle.Render("+" + strings.Repeat("-", innerWidth+2) + "+")
	fmt.Println(topBorder)

	// Helper to print a line within the box
	printBoxLine := func(content string, style lipgloss.Style) {
		// Truncate if needed
		if len(content) > innerWidth {
			content = content[:innerWidth-3] + "..."
		}
		// Pad to fill width
		padded := content + strings.Repeat(" ", innerWidth-len(content))
		fmt.Printf("%s %s %s\n",
			borderStyle.Render("|"),
			style.Render(padded),
			borderStyle.Render("|"))
	}

	// Helper to print host context lines (dimmed)
	printContextLines := func(lines []string, maxLines int, position string) {
		if len(lines) == 0 {
			return
		}

		// Truncate to maxLines
		showLines := lines
		truncated := false
		if len(lines) > maxLines {
			if position == "before" {
				// Show last N lines for "before" context
				showLines = lines[len(lines)-maxLines:]
				truncated = true
			} else {
				// Show first N lines for "after" context
				showLines = lines[:maxLines]
				truncated = true
			}
		}

		if truncated && position == "before" {
			printBoxLine("  ...", dimStyle)
		}

		for _, line := range showLines {
			// Remove trailing newline for display
			line = strings.TrimSuffix(line, "\n")
			printBoxLine(line, dimStyle)
		}

		if truncated && position == "after" {
			printBoxLine("  ...", dimStyle)
		}
	}

	// Before context (dimmed)
	printContextLines(beforeHost, maxContextLines, "before")

	// Blank separator line if we have before context
	if len(beforeHost) > 0 {
		printBoxLine("", dimStyle)
	}

	// New host lines (green with + prefix)
	for _, line := range newLines {
		line = strings.TrimSuffix(line, "\n")
		if strings.TrimSpace(line) == "" {
			printBoxLine("", addStyle)
		} else {
			printBoxLine("+ "+line, addStyle)
		}
	}

	// Blank separator line if we have after context
	if len(afterHost) > 0 {
		printBoxLine("", dimStyle)
	}

	// After context (dimmed)
	printContextLines(afterHost, maxContextLines, "after")

	// Bottom border
	bottomBorder := borderStyle.Render("+" + strings.Repeat("-", innerWidth+2) + "+")
	fmt.Println(bottomBorder)
	fmt.Println()
}

// RemovalPreview displays a diff-style preview of a host being removed.
func RemovalPreview(hostLines []string, targetFile string) {
	// Styles
	removeStyle := lipgloss.NewStyle().Foreground(ColorRed)
	borderStyle := lipgloss.NewStyle().Foreground(ColorDim)

	width := termWidth()
	innerWidth := width - 4

	// Header
	Warning("Planning host entry removal from %s", abbreviateHome(targetFile))
	fmt.Println()

	// Top border
	topBorder := borderStyle.Render("+" + strings.Repeat("-", innerWidth+2) + "+")
	fmt.Println(topBorder)

	// Print each line with - prefix (red)
	for _, line := range hostLines {
		line = strings.TrimSuffix(line, "\n")
		content := "- " + line
		if len(content) > innerWidth {
			content = content[:innerWidth-3] + "..."
		}
		padded := content + strings.Repeat(" ", innerWidth-len(content))
		fmt.Printf("%s %s %s\n",
			borderStyle.Render("|"),
			removeStyle.Render(padded),
			borderStyle.Render("|"))
	}

	// Bottom border
	bottomBorder := borderStyle.Render("+" + strings.Repeat("-", innerWidth+2) + "+")
	fmt.Println(bottomBorder)
	fmt.Println()
}

// CompatFixPreview displays what compatibility fixes will be applied.
func CompatFixPreview(hostname string, fixes []string) {
	if len(fixes) == 0 {
		return
	}

	fmt.Println()
	Info("Compatibility fixes to apply for %s:", hostname)
	for _, fix := range fixes {
		fmt.Printf("    %s %s\n", Gray("-"), fix)
	}
}

// abbreviateHome replaces the home directory with ~ in a path.
func abbreviateHome(path string) string {
	home := homeDir()
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// homeDir returns the user's home directory or empty string.
func homeDir() string {
	return os.Getenv("HOME")
}
