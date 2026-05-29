package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var updateSnapshots = flag.Bool("update-snapshots", false, "Update help snapshots instead of comparing")

// repoRoot returns the repository root directory
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// snapshotDir returns the path to the help snapshots directory
func snapshotDir() string {
	return filepath.Join(repoRoot(), "docs", "examples", "help")
}

// discoverCommands recursively discovers all commands and returns a map of
// snapshot path -> command path (e.g., "host/add.txt" -> ["host", "add"])
func discoverCommands(cmd *cobra.Command, prefix []string) map[string][]string {
	cases := make(map[string][]string)

	for _, sub := range cmd.Commands() {
		if sub.Hidden {
			continue
		}

		cmdPath := append(append([]string{}, prefix...), sub.Name())

		// Build snapshot path
		var snapshotPath string
		if len(prefix) == 0 {
			snapshotPath = sub.Name() + ".txt"
		} else {
			snapshotPath = filepath.Join(append(prefix, sub.Name()+".txt")...)
		}

		cases[snapshotPath] = cmdPath

		// Recurse into subcommands
		if sub.HasSubCommands() {
			for k, v := range discoverCommands(sub, cmdPath) {
				cases[k] = v
			}
		}
	}

	return cases
}

// buildBinary builds the nssh binary and returns its path
func buildBinary(t *testing.T) string {
	t.Helper()

	binPath := filepath.Join(t.TempDir(), "nssh")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", binPath, "./cmd/nssh")
	cmd.Dir = repoRoot()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\n%s", err, output)
	}
	return binPath
}

// captureHelp runs the binary with --help and captures output
func captureHelp(binPath string, cmdPath []string) (string, error) {
	// Build command args
	args := append([]string{}, cmdPath...)
	args = append(args, "--help")

	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), "COLUMNS=80") // Consistent width

	output, err := cmd.CombinedOutput()
	if err != nil {
		// --help returns exit code 0, so any error is unexpected
		return "", fmt.Errorf("command failed: %v\n%s", err, output)
	}

	// Build user-facing command string for header
	userCmd := "nssh"
	if len(cmdPath) > 0 {
		userCmd += " " + strings.Join(cmdPath, " ")
	}
	userCmd += " --help"

	// Normalize line endings and add header
	result := strings.ReplaceAll(string(output), "\r\n", "\n")
	return fmt.Sprintf("$ %s\n%s", userCmd, result), nil
}

// readSnapshot reads a snapshot file
func readSnapshot(name string) (string, error) {
	path := filepath.Join(snapshotDir(), name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// writeSnapshot writes a snapshot file
func writeSnapshot(name, content string) error {
	path := filepath.Join(snapshotDir(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func TestHelpSnapshots(t *testing.T) {
	// Build binary once for all tests
	binPath := buildBinary(t)

	// Discover all commands from Cobra structure
	rootCmd := newRootCmd()
	cases := discoverCommands(rootCmd, nil)

	// Add root command
	cases["nssh.txt"] = []string{}

	for snapshotPath, cmdPath := range cases {
		t.Run(snapshotPath, func(t *testing.T) {
			observed, err := captureHelp(binPath, cmdPath)
			if err != nil {
				t.Fatalf("Failed to capture help: %v", err)
			}

			if *updateSnapshots {
				if err := writeSnapshot(snapshotPath, observed); err != nil {
					t.Fatalf("Failed to write snapshot: %v", err)
				}
				t.Logf("Updated: %s", snapshotPath)
				return
			}

			expected, err := readSnapshot(snapshotPath)
			if err != nil {
				if os.IsNotExist(err) {
					t.Fatalf("Missing snapshot: %s\nRun: go test ./cmd/nssh -args -update-snapshots", snapshotPath)
				}
				t.Fatalf("Failed to read snapshot: %v", err)
			}

			if observed != expected {
				t.Errorf("Help output drifted from %s\nRun: go test ./cmd/nssh -args -update-snapshots\n\nExpected:\n%s\n\nObserved:\n%s",
					snapshotPath, expected, observed)
			}
		})
	}
}

// TestMain handles the -update-snapshots flag
func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}
