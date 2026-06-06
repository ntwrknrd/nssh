package bench

import (
	"context"
	"fmt"
	"time"

	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

var runIntegratedCredentialSSHBenchmark = runSSHBenchmarkWithOptions

func NewCredentialCmd() *cobra.Command {
	var (
		warmups int
		samples int
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "credential <host>",
		Short: "Benchmark credential auth path",
		Long: `Measure credential-backed password authentication timing.

The benchmark resolves the host through nssh inventory/auth configuration, then
runs both the isolated credential lookup and an integrated SSH password-auth
benchmark using the same credential resolver.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCredentialBenchmark(args[0], warmups, samples, timeout)
		},
	}

	cmd.Flags().IntVar(&warmups, "warmups", 1, "number of warmup runs (not measured)")
	cmd.Flags().IntVar(&samples, "samples", 3, "number of measured runs")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "maximum time for each credential lookup")

	return cmd
}

func runCredentialBenchmark(host string, warmups, samples int, timeout time.Duration) error {
	if samples < 1 {
		return fmt.Errorf("--samples must be at least 1")
	}
	if warmups < 0 {
		return fmt.Errorf("--warmups must be >= 0")
	}
	if timeout <= 0 {
		return fmt.Errorf("--timeout must be > 0")
	}

	target, err := resolveCredentialBenchmarkTarget(host)
	if err != nil {
		return err
	}

	return runCredentialBenchmarkForTarget(target, warmups, samples, timeout)
}

func resolveCredentialBenchmarkTarget(host string) (*connect.CredentialTarget, error) {
	resolvedHost, err := resolveBenchmarkHost(host)
	if err != nil {
		return nil, err
	}
	resolved, err := connect.ResolveHostForConnect(resolvedHost, "")
	if err != nil {
		return nil, err
	}
	return connect.CredentialTargetFromResolvedHost(resolved)
}

func runCredentialBenchmarkForTarget(target *connect.CredentialTarget, warmups, samples int, timeout time.Duration) error {
	ui.CommandStart("CREDENTIAL AUTH BENCHMARK")
	printCredentialTarget(target)
	fmt.Printf("  %s: %d samples, %d warmup, %s timeout\n", ui.Gray("Config"), samples, warmups, timeout)
	fmt.Println()

	ui.SubSection("Credential Lookup")
	result, err := runCredentialTargetBenchmarkWithTimeout(context.Background(), target, warmups, samples, timeout)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	renderResults(result, false)
	PrintSavedPath(SaveResultsWithMetadata("credential", target.Host, result, false, credentialMetadata(target)))

	ui.SubSection("Integrated SSH Password Auth")
	if err := runIntegratedCredentialSSHBenchmark(target.Host, warmups, samples, false, true); err != nil {
		return err
	}
	return nil
}

func runCredentialTargetBenchmark(ctx context.Context, target *connect.CredentialTarget, warmups, samples int) (*BenchmarkResult, error) {
	return runCredentialTargetBenchmarkWithTimeout(ctx, target, warmups, samples, 10*time.Second)
}

func runCredentialTargetBenchmarkWithTimeout(ctx context.Context, target *connect.CredentialTarget, warmups, samples int, timeout time.Duration) (*BenchmarkResult, error) {
	if target == nil || (target.Password == nil && target.Resolver == nil) {
		host := ""
		if target != nil {
			host = target.Host
		}
		return nil, fmt.Errorf("host %s has no credential resolver", host)
	}
	result := &BenchmarkResult{
		WarmupRuns:   warmups,
		MeasuredRuns: samples,
		TotalRuns:    warmups + samples,
		StageNames:   []string{connector.TimingCredentialLookup},
	}

	for i := 0; i < result.TotalRuns; i++ {
		isWarmup := i < warmups
		start := time.Now()
		lookupCtx, cancel := context.WithTimeout(ctx, timeout)
		resolved, err := resolveCredentialForBenchmark(lookupCtx, target)
		cancel()
		duration := time.Since(start)
		if err != nil {
			return nil, err
		}
		if resolved != nil && target.Resolver != nil {
			resolved.Destroy()
		}

		if isWarmup {
			fmt.Printf("  Warmup %d/%d: %s\n", i+1, warmups, ui.Cyan(formatDuration(duration)))
			continue
		}

		result.Samples = append(result.Samples, map[string]time.Duration{
			connector.TimingCredentialLookup: duration,
		})
		result.WallClocks = append(result.WallClocks, duration)
		sampleNum := i - warmups + 1
		fmt.Printf("  Sample %d/%d: %s\n", sampleNum, samples, ui.Cyan(formatDuration(duration)))
	}
	return result, nil
}

func resolveCredentialForBenchmark(ctx context.Context, target *connect.CredentialTarget) (*secret.Secret, error) {
	if target.Resolver != nil {
		return target.Resolver(ctx)
	}
	return target.Password, nil
}

func printCredentialTarget(target *connect.CredentialTarget) {
	fmt.Printf("  %s: %s\n", ui.Gray("Host"), ui.Cyan(target.Host))
	fmt.Printf("  %s: %s\n", ui.Gray("Provider"), safeValue(target.Provider))
	fmt.Printf("  %s: %s\n", ui.Gray("Source"), safeValue(target.Source))
	fmt.Printf("  %s: %s\n", ui.Gray("Ref"), safeValue(target.RefKind))
	fmt.Printf("  %s: %t\n", ui.Gray("Username"), target.UsernamePresent)
}

func credentialMetadata(target *connect.CredentialTarget) map[string]string {
	return map[string]string{
		"provider":         target.Provider,
		"source":           target.Source,
		"ref_kind":         target.RefKind,
		"username_present": fmt.Sprintf("%t", target.UsernamePresent),
	}
}

func safeValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
