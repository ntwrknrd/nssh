package captured

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestBoundedCaptureDrainsLongLinesAndKeepsExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := defaultExec(ctx, Command{Name: "sh", Args: []string{"-c", "cat; printf failure >&2; exit 7"}, Stdin: bytes.NewReader(bytes.Repeat([]byte{'x'}, 2<<20)), MaxOutputBytes: 1024})
	if result.ExitCode != 7 || result.Err == nil || !result.Truncated {
		t.Fatalf("exit=%d err=%v truncated=%v", result.ExitCode, result.Err, result.Truncated)
	}
	if len(result.Stdout)+len(result.Stderr) != 1024 || len(result.Output) != 0 {
		t.Fatalf("retained=%d events=%d", len(result.Stdout)+len(result.Stderr), len(result.Output))
	}
}

func TestBoundedCaptureKeepsSeparateSmallStreams(t *testing.T) {
	result := defaultExec(context.Background(), Command{Name: "sh", Args: []string{"-c", "printf data; printf error >&2"}, MaxOutputBytes: 1024})
	if result.Err != nil || result.Truncated || string(result.Stdout) != "data" || string(result.Stderr) != "error" {
		t.Fatalf("result=%+v", result)
	}
}
