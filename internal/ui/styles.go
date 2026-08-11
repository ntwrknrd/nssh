// Package ui provides terminal user interface components including styled text,
// panels, tables, prompts, and progress bars using lipgloss and huh.
package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// Color palette - muted, professional tones
var (
	ColorCyan    = lipgloss.Color("6")   // ANSI cyan
	ColorGreen   = lipgloss.Color("78")  // Soft green
	ColorYellow  = lipgloss.Color("220") // Warning yellow
	ColorRed     = lipgloss.Color("196") // Error red
	ColorGray    = lipgloss.Color("245") // Muted gray
	ColorDim     = lipgloss.Color("240") // Dimmer gray for borders
	ColorWhite   = lipgloss.Color("255") // Bright white
	ColorMagenta = lipgloss.Color("213") // Soft magenta
)

// Text styles
var (
	StyleBold      = lipgloss.NewStyle().Bold(true)
	StyleDim       = lipgloss.NewStyle().Foreground(ColorGray)
	StyleCyan      = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleGreen     = lipgloss.NewStyle().Foreground(ColorGreen)
	StyleYellow    = lipgloss.NewStyle().Foreground(ColorYellow)
	StyleRed       = lipgloss.NewStyle().Foreground(ColorRed)
	StyleMagenta   = lipgloss.NewStyle().Foreground(ColorMagenta)
	StyleWhite     = lipgloss.NewStyle().Foreground(ColorWhite)
	StyleLabel     = lipgloss.NewStyle().Foreground(ColorGray).Bold(true)
	StyleValue     = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleValueDim  = lipgloss.NewStyle().Foreground(ColorGray)
	StyleHighlight = lipgloss.NewStyle().Foreground(ColorGreen).Bold(true)
)

// Cyan renders text in cyan color.
func Cyan(s string) string { return StyleCyan.Render(s) }

// Yellow renders text in yellow color.
func Yellow(s string) string { return StyleYellow.Render(s) }

// Red renders text in red color.
func Red(s string) string { return StyleRed.Render(s) }

// Gray renders text in gray color.
func Gray(s string) string { return StyleDim.Render(s) }

// DimCyan renders text in dimmed cyan color.
func DimCyan(s string) string { return StyleCyan.Faint(true).Render(s) }

// DimGreen renders text in dimmed green color.
func DimGreen(s string) string { return StyleGreen.Faint(true).Render(s) }

// DimRed renders text in dimmed red color.
func DimRed(s string) string { return StyleRed.Faint(true).Render(s) }
