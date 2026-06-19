//go:build unix

package connector

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/creack/pty"
)

func TestStartPTYInheritsCurrentTerminalSizeBeforeChildRuns(t *testing.T) {
	stdinPTY, stdinTTY, err := pty.Open()
	if err != nil {
		t.Fatalf("open stdin pty: %v", err)
	}
	defer func() { _ = stdinPTY.Close() }()
	defer func() { _ = stdinTTY.Close() }()

	want := &pty.Winsize{Rows: 33, Cols: 132}
	if err := pty.Setsize(stdinTTY, want); err != nil {
		t.Fatalf("set stdin tty size: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = stdinTTY
	t.Cleanup(func() { os.Stdin = oldStdin })

	cmd := exec.Command("sh", "-c", "stty size")
	childPTY, err := startPTYWithInheritedSize(cmd)
	if err != nil {
		t.Fatalf("start pty: %v", err)
	}
	defer func() { _ = childPTY.Close() }()

	out, err := io.ReadAll(childPTY)
	if err != nil {
		t.Fatalf("read child pty: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait child: %v", err)
	}

	if got := strings.TrimSpace(string(out)); got != "33 132" {
		t.Fatalf("child stty size = %q, want 33 132", got)
	}
}

func TestLogOpenSSHCommandIncludesExecutableAndFullArgv(t *testing.T) {
	handler := &captureSlogHandler{}
	original := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(original) })

	logOpenSSHCommand([]string{"-tt", "-F", "none", "-o", "IdentityAgent=/tmp/agent sock", "edge01"})

	if len(handler.records) != 1 {
		t.Fatalf("records = %d, want 1", len(handler.records))
	}
	record := handler.records[0]
	if record.Message != "executing openssh" {
		t.Fatalf("message = %q, want executing openssh", record.Message)
	}
	attrs := recordAttrs(record)
	got, ok := attrs["argv"].Any().([]string)
	if !ok {
		t.Fatalf("argv attr type = %T, want []string", attrs["argv"].Any())
	}
	want := []string{"ssh", "-tt", "-F", "none", "-o", "IdentityAgent=/tmp/agent sock", "edge01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

type captureSlogHandler struct {
	records []slog.Record
}

func (h *captureSlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *captureSlogHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record.Clone())
	return nil
}

func (h *captureSlogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *captureSlogHandler) WithGroup(string) slog.Handler {
	return h
}

func recordAttrs(record slog.Record) map[string]slog.Value {
	attrs := make(map[string]slog.Value)
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value
		return true
	})
	return attrs
}
