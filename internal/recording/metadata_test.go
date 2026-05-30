//go:build unix

package recording

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionUpdatedTimestampUsesCastMtime(t *testing.T) {
	dir := t.TempDir()
	castPath := filepath.Join(dir, "session-001.cast")
	if err := os.WriteFile(castPath, []byte("{}\n"), 0600); err != nil {
		t.Fatalf("write cast: %v", err)
	}
	mtime := time.Date(2026, 5, 29, 12, 30, 0, 0, time.UTC)
	if err := os.Chtimes(castPath, mtime, mtime); err != nil {
		t.Fatalf("set cast mtime: %v", err)
	}

	finishedAt := time.Date(2026, 5, 28, 12, 30, 0, 0, time.UTC)
	got := SessionUpdatedTimestamp(SessionRecord{CastPath: castPath, FinishedAt: finishedAt})
	if !got.Equal(mtime) {
		t.Fatalf("SessionUpdatedTimestamp() = %v, want %v", got, mtime)
	}
}

func TestSessionUpdatedTimestampFallsBackToFinishedAt(t *testing.T) {
	finishedAt := time.Date(2026, 5, 28, 12, 30, 0, 0, time.UTC)
	got := SessionUpdatedTimestamp(SessionRecord{CastPath: filepath.Join(t.TempDir(), "missing.cast"), FinishedAt: finishedAt})
	if !got.Equal(finishedAt) {
		t.Fatalf("SessionUpdatedTimestamp() = %v, want %v", got, finishedAt)
	}
}

func TestSessionDurationSecondsUsesIndexSessions(t *testing.T) {
	dir := t.TempDir()
	castPath := filepath.Join(dir, "session-001.cast")
	if err := os.WriteFile(castPath, []byte("{}\n"), 0600); err != nil {
		t.Fatalf("write cast: %v", err)
	}
	startedAt := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	if err := WriteIndex(castPath, "edge01", startedAt, startedAt.Add(2*time.Minute), 0, "", []string{"edge01"}, "session-001"); err != nil {
		t.Fatalf("write first index entry: %v", err)
	}
	if err := WriteIndex(castPath, "edge01", startedAt.Add(5*time.Minute), startedAt.Add(8*time.Minute), 0, "", []string{"edge01"}, "session-001"); err != nil {
		t.Fatalf("write second index entry: %v", err)
	}

	got := SessionDurationSeconds(SessionRecord{
		CastPath:   castPath,
		StartedAt:  startedAt,
		FinishedAt: startedAt.Add(30 * time.Second),
	})
	if got != 300 {
		t.Fatalf("SessionDurationSeconds() = %d, want 300", got)
	}
}

func TestSessionDurationSecondsFallsBackToRecordTimestamps(t *testing.T) {
	startedAt := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	got := SessionDurationSeconds(SessionRecord{
		CastPath:   filepath.Join(t.TempDir(), "missing.cast"),
		StartedAt:  startedAt,
		FinishedAt: startedAt.Add(90 * time.Second),
	})
	if got != 90 {
		t.Fatalf("SessionDurationSeconds() = %d, want 90", got)
	}
}
