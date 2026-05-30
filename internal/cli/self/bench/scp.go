package bench

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewSCPCmd creates the scp benchmark subcommand.
func NewSCPCmd() *cobra.Command {
	var (
		warmups    int
		samples    int
		simpleOnly bool
		fileSize   string
	)

	cmd := &cobra.Command{
		Use:   "scp <host>",
		Short: "Benchmark SCP transfers",
		Long: `Measure SCP file transfer timing overhead.

This command transfers a test file to/from the specified host and measures:
- Connection establishment time
- File transfer time
- Total transfer time

The benchmark creates a temporary test file, transfers it to the remote host,
then downloads it back and compares transfer times.

Use --simple for wall-clock only measurements (faster).
Use --size to specify test file size (e.g., "1K", "1M", "10M").

Examples:
  nssh self bench scp router1
  nssh self bench scp router1 --samples 5
  nssh self bench scp router1 --size 1M`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSCPBenchmark(args[0], warmups, samples, simpleOnly, fileSize)
		},
	}

	cmd.Flags().IntVar(&warmups, "warmups", 1, "number of warmup runs (not measured)")
	cmd.Flags().IntVar(&samples, "samples", 3, "number of measured runs")
	cmd.Flags().BoolVar(&simpleOnly, "simple", false, "wall-clock timing only")
	cmd.Flags().StringVar(&fileSize, "size", "1K", "test file size (e.g., 1K, 1M)")

	return cmd
}

func runSCPBenchmark(host string, warmups, samples int, simpleOnly bool, fileSize string) error {
	if samples < 1 {
		return fmt.Errorf("--samples must be at least 1")
	}
	if warmups < 0 {
		return fmt.Errorf("--warmups must be >= 0")
	}

	ui.CommandStart("SCP BENCHMARK")

	fmt.Printf("  %s: %s\n", ui.Gray("Host"), ui.Cyan(host))
	fmt.Printf("  %s: %d samples, %d warmup, %s file\n", ui.Gray("Config"), samples, warmups, fileSize)
	fmt.Println()

	// Create temporary test file
	tempDir, err := os.MkdirTemp("", "nssh-benchmark-*")
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	testFile := filepath.Join(tempDir, "nssh-benchmark-test")
	if err := createTestFile(testFile, fileSize); err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("create test file: %w", err)
	}

	// First, upload the test file to remote
	remoteFile := "/tmp/nssh-benchmark-test"
	uploadArgs := []string{"cp", testFile, fmt.Sprintf("%s:%s", host, remoteFile)}

	fmt.Printf("  %s\n", ui.Gray("── Upload ──"))
	result, err := run(uploadArgs, warmups, samples, simpleOnly)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("upload benchmark failed: %w", err)
	}
	renderResults(result, simpleOnly)
	PrintSavedPath(SaveResults("scp-upload", host, result, simpleOnly))

	// Download benchmark
	downloadFile := filepath.Join(tempDir, "downloaded")
	downloadArgs := []string{"cp", fmt.Sprintf("%s:%s", host, remoteFile), downloadFile}

	fmt.Println()
	fmt.Printf("  %s\n", ui.Gray("── Download ──"))
	downloadResult, err := run(downloadArgs, warmups, samples, simpleOnly)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("download benchmark failed: %w", err)
	}
	renderResults(downloadResult, simpleOnly)
	PrintSavedPath(SaveResults("scp-download", host, downloadResult, simpleOnly))

	// Cleanup remote file
	cleanupArgs := []string{host, "--", "rm", "-f", remoteFile}
	binary, _ := findBinary()
	if binary != "" {
		_ = exec.Command(binary, cleanupArgs...).Run()
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

// createTestFile creates a test file of the specified size.
func createTestFile(path string, size string) error {
	// Parse size string (e.g., "1K", "1M", "10M")
	bytes, err := parseSize(size)
	if err != nil {
		return err
	}

	// Create file with random data
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// Write in chunks
	chunk := make([]byte, 4096)
	for i := range chunk {
		chunk[i] = byte(i % 256)
	}

	remaining := bytes
	for remaining > 0 {
		writeSize := len(chunk)
		if remaining < int64(writeSize) {
			writeSize = int(remaining)
		}
		n, err := f.Write(chunk[:writeSize])
		if err != nil {
			return err
		}
		remaining -= int64(n)
	}

	return nil
}

// parseSize parses a size string like "1K", "1M", "10M" into bytes.
func parseSize(s string) (int64, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("empty size")
	}

	multiplier := int64(1)
	numStr := s

	switch s[len(s)-1] {
	case 'K', 'k':
		multiplier = 1024
		numStr = s[:len(s)-1]
	case 'M', 'm':
		multiplier = 1024 * 1024
		numStr = s[:len(s)-1]
	case 'G', 'g':
		multiplier = 1024 * 1024 * 1024
		numStr = s[:len(s)-1]
	}

	var num int64
	_, err := fmt.Sscanf(numStr, "%d", &num)
	if err != nil {
		return 0, fmt.Errorf("invalid size: %s", s)
	}

	return num * multiplier, nil
}
