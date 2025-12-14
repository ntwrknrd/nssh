package self

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

// NewVersionCmd creates the version subcommand (hidden, use -V flag instead).
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "version",
		Short:  "Show version info",
		Long:   `Display the nssh version, Go version, and platform information.`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			RunVersion()
			return nil
		},
	}
}

// RunVersion prints version information and can be called from the root command's
// version flag handler.
func RunVersion() {
	fmt.Printf("nssh version %s (", version)
	if commit != "" {
		fmt.Printf("%s, ", commit)
	}
	if date != "" {
		fmt.Printf("%s, ", date)
	}
	fmt.Printf("%s, %s/%s)", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if features != "" {
		fmt.Printf(" [%s]", features)
	}
	fmt.Println()
}

// RunVersionExit prints version and exits. Used by the root command's persistent flag.
func RunVersionExit() {
	RunVersion()
	os.Exit(0)
}
