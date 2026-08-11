package log

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/recording"
)

func TestPrintSessionsOmitsSessionColumn(t *testing.T) {
	castPath := filepath.Join(t.TempDir(), "session-000.cast")
	if err := os.WriteFile(castPath, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 6, 2, 57, 20, 0, time.Local)
	if err := os.Chtimes(castPath, now, now); err != nil {
		t.Fatal(err)
	}

	got := captureStdout(t, func() {
		PrintSessions([]recording.SessionRecord{
			{
				Host:         "acm-lab-agg-sw1.custcbb.local",
				CastPath:     castPath,
				StartedAt:    now.Add(-3 * time.Second),
				FinishedAt:   now,
				SessionLabel: "session-000",
			},
		}, "")
	})

	// Check the header row only: the Cast column truncates the temp path,
	// which contains this test's name and therefore the word "Session".
	header := ""
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "Last Updated") {
			header = line
			break
		}
	}
	if header == "" {
		t.Fatalf("log list table missing header row:\n%s", got)
	}
	if strings.Contains(header, "Session") {
		t.Fatalf("log list table should not include a Session column:\n%s", got)
	}
	for _, want := range []string{"Last Updated", "Host", "Duration", "Cast"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log list table missing %q:\n%s", want, got)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = original })

	fn()

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
