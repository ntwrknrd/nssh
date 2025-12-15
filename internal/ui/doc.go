// Package ui provides terminal user interface components and utilities.
//
// This package contains all terminal-facing UI elements used by nssh CLI commands,
// including styled output, prompts, tables, and fuzzy selection.
//
// # Output Functions
//
// Styled output functions for consistent CLI appearance:
//
//	ui.Info("Processing %d hosts", count)     // Blue info message
//	ui.Success("Connection established")       // Green success message
//	ui.Warning("Certificate expires soon")     // Yellow warning
//	ui.Error("Connection failed: %v", err)     // Red error message
//
// # Prompts
//
// Interactive prompts for user input:
//
//	name, err := ui.Prompt("Hostname", "default-value")
//	password, err := ui.PasswordSecure("Password")  // Returns *memguard.LockedBuffer
//	confirmed, err := ui.Confirm("Delete host?", false)
//
// # Fuzzy Selection
//
// Integration with fzf for fuzzy selection:
//
//	selected, err := ui.FuzzySelectString("Select host", hosts, "")
//
// # Tables
//
// Formatted table output:
//
//	t := ui.NewTable("Name", "Host", "Port")
//	t.AddRow("server1", "10.0.0.1", "22")
//	t.Print()
//
// # Styled Help
//
// Custom help formatting for Cobra commands:
//
//	ui.ApplyStyledHelp(cmd)           // Single command
//	ui.ApplyStyledHelpRecursive(cmd)  // Command and all subcommands
//
// # Components
//
// Visual components for command output:
//
//	ui.Ruler("SECTION TITLE")   // Horizontal rule with title
//	ui.CommandStart("CONNECT")  // Command header
//	ui.CommandEnd(ui.StatusSuccess)  // Command footer with status
package ui
