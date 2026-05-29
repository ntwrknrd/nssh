package ui

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRenderStyledHelp_BasicCommand(t *testing.T) {
	cmd := &cobra.Command{
		Use:   "test [ARG]",
		Short: "A test command",
		Long:  "This is a longer description of the test command.",
	}
	cmd.Flags().StringP("name", "n", "", "The name to use")
	cmd.Flags().BoolP("verbose", "v", false, "Enable verbose output")

	cfg := StyledHelpConfig{
		ShowGlobalFlags: false,
		Width:           80,
	}

	output := RenderStyledHelp(cmd, cfg)

	// Check for expected elements
	if !strings.Contains(output, "Usage") {
		t.Error("Expected Usage panel")
	}
	if !strings.Contains(output, "Options") {
		t.Error("Expected Options panel")
	}
	if !strings.Contains(output, "test [ARG]") {
		t.Error("Expected usage line")
	}
	if !strings.Contains(output, "--name") {
		t.Error("Expected --name flag")
	}
	if !strings.Contains(output, "-n") {
		t.Error("Expected -n shorthand")
	}
	if !strings.Contains(output, "STRING") {
		t.Error("Expected STRING type indicator")
	}
}

func TestRenderStyledHelp_WithGlobalFlags(t *testing.T) {
	root := &cobra.Command{
		Use:   "root",
		Short: "Root command",
	}
	root.PersistentFlags().BoolP("debug", "d", false, "Enable debug mode")

	child := &cobra.Command{
		Use:   "child",
		Short: "Child command",
	}
	child.Flags().StringP("output", "o", "", "Output file")
	root.AddCommand(child)

	cfg := StyledHelpConfig{
		ShowGlobalFlags: true,
		Width:           80,
	}

	output := RenderStyledHelp(child, cfg)

	// Check for both local and global options
	if !strings.Contains(output, "Options") {
		t.Error("Expected Options panel")
	}
	if !strings.Contains(output, "Global Options") {
		t.Error("Expected Global Options panel")
	}
	if !strings.Contains(output, "--output") {
		t.Error("Expected local flag --output")
	}
	if !strings.Contains(output, "--debug") {
		t.Error("Expected inherited flag --debug")
	}
}

func TestRenderStyledHelp_AnnotatedUsageLines(t *testing.T) {
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Show details",
		Annotations: map[string]string{
			UsageLinesAnnotation: "nssh inv get HOST\nnssh inv get -g GROUP",
		},
	}
	cmd.Flags().BoolP("group", "g", false, "treat argument as a group name")

	output := RenderStyledHelp(cmd, StyledHelpConfig{ShowGlobalFlags: false, Width: 80})
	for _, want := range []string{
		"nssh inv get HOST",
		"nssh inv get -g GROUP",
		"-g, --group",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("help missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "get NAME [flags]") {
		t.Fatalf("help used generic cobra usage:\n%s", output)
	}
}

func TestFormatFlagName(t *testing.T) {
	tests := []struct {
		name      string
		shorthand string
		flagType  string
		expected  string
	}{
		{
			name:      "verbose",
			shorthand: "v",
			flagType:  "bool",
			expected:  "-v, --verbose",
		},
		{
			name:      "output",
			shorthand: "o",
			flagType:  "string",
			expected:  "-o, --output STRING",
		},
		{
			name:      "count",
			shorthand: "",
			flagType:  "int",
			expected:  "--count INT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "test"}
			switch tt.flagType {
			case "bool":
				cmd.Flags().BoolP(tt.name, tt.shorthand, false, "test flag")
			case "string":
				cmd.Flags().StringP(tt.name, tt.shorthand, "", "test flag")
			case "int":
				if tt.shorthand != "" {
					cmd.Flags().IntP(tt.name, tt.shorthand, 0, "test flag")
				} else {
					cmd.Flags().Int(tt.name, 0, "test flag")
				}
			}

			flag := cmd.Flags().Lookup(tt.name)
			result := formatFlagName(flag)

			if result != tt.expected {
				t.Errorf("formatFlagName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFlagTypeString(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("bool", false, "")
	cmd.Flags().String("string", "", "")
	cmd.Flags().Int("int", 0, "")
	cmd.Flags().Int64("int64", 0, "")
	cmd.Flags().Float64("float", 0, "")
	cmd.Flags().StringSlice("slice", nil, "")

	tests := []struct {
		flagName string
		expected string
	}{
		{"bool", ""},
		{"string", "STRING"},
		{"int", "INT"},
		{"int64", "INT"},
		{"float", "FLOAT"},
		{"slice", "STRINGS"},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := cmd.Flags().Lookup(tt.flagName)
			result := flagTypeString(flag)
			if result != tt.expected {
				t.Errorf("flagTypeString(%s) = %q, want %q", tt.flagName, result, tt.expected)
			}
		})
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		input    string
		width    int
		expected string
	}{
		{"hello", 10, "hello     "},
		{"hello", 5, "hello"},
		{"hello", 3, "hello"}, // Doesn't truncate
		{"", 5, "     "},
	}

	for _, tt := range tests {
		result := padRight(tt.input, tt.width)
		if result != tt.expected {
			t.Errorf("padRight(%q, %d) = %q, want %q", tt.input, tt.width, result, tt.expected)
		}
	}
}

func TestRenderPanel(t *testing.T) {
	content := "  -v, --verbose  Enable verbose output"
	panel := renderPanel("Options", content, 60)

	// Check for panel structure
	if !strings.Contains(panel, "╭") {
		t.Error("Expected top-left corner")
	}
	if !strings.Contains(panel, "╯") {
		t.Error("Expected bottom-right corner")
	}
	if !strings.Contains(panel, "Options") {
		t.Error("Expected title in panel")
	}
	if !strings.Contains(panel, "--verbose") {
		t.Error("Expected content in panel")
	}
}

func TestApplyStyledHelpRecursive(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	child1 := &cobra.Command{Use: "child1"}
	child2 := &cobra.Command{Use: "child2"}
	grandchild := &cobra.Command{Use: "grandchild"}

	root.AddCommand(child1)
	root.AddCommand(child2)
	child1.AddCommand(grandchild)

	ApplyStyledHelpRecursive(root)

	// Verify help func is set on all commands
	if root.HelpFunc() == nil {
		t.Error("Expected help func on root")
	}
	if child1.HelpFunc() == nil {
		t.Error("Expected help func on child1")
	}
	if child2.HelpFunc() == nil {
		t.Error("Expected help func on child2")
	}
	if grandchild.HelpFunc() == nil {
		t.Error("Expected help func on grandchild")
	}
}

func TestRenderStyledHelp_LongDescriptionHidden(t *testing.T) {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "A test command",
		Long:  "This is extended help that should only appear with --explain.",
	}

	// Help output should NOT show long description (use --explain instead)
	cfg := DefaultHelpConfig()
	cfg.Width = 80
	output := RenderStyledHelp(cmd, cfg)

	if strings.Contains(output, "extended help") {
		t.Error("Long description should be hidden in help output")
	}
}

func TestApplyStyledHelp_ExplainFlag(t *testing.T) {
	// Command WITH Long description should get --explain flag
	cmdWithLong := &cobra.Command{
		Use:   "test",
		Short: "Short desc",
		Long:  "Extended description here.",
	}
	ApplyStyledHelp(cmdWithLong)

	explainFlag := cmdWithLong.Flags().Lookup("explain")
	if explainFlag == nil {
		t.Error("Expected --explain flag on command with Long description")
	}

	// Should also have PreRunE set
	if cmdWithLong.PreRunE == nil {
		t.Error("Expected PreRunE to be set for --explain handling")
	}

	// Command WITHOUT Long description should NOT get --explain flag
	cmdNoLong := &cobra.Command{
		Use:   "test2",
		Short: "Short desc only",
	}
	ApplyStyledHelp(cmdNoLong)

	noExplainFlag := cmdNoLong.Flags().Lookup("explain")
	if noExplainFlag != nil {
		t.Error("Did not expect --explain flag on command without Long description")
	}
}

func TestIsExplainShown(t *testing.T) {
	// Create command with explain flag
	cmd := &cobra.Command{
		Use:  "test",
		Long: "Extended help.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	ApplyStyledHelp(cmd)

	// Simulate --explain being set
	cmd.SetArgs([]string{"--explain"})
	err := cmd.Execute()

	if err == nil {
		t.Error("Expected error from --explain handling")
	}
	if !IsExplainShown(err) {
		t.Errorf("Expected IsExplainShown to return true, got error: %v", err)
	}
}
