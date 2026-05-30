//go:build linux || darwin

package recording

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*fakeTimer]struct{}
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{
		now:    stripMonotonic(start),
		timers: make(map[*fakeTimer]struct{}),
	}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(d time.Duration) ClockTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{
		clock:    c,
		ch:       make(chan time.Time, 1),
		deadline: c.now.Add(d),
	}
	c.timers[t] = struct{}{}
	return t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	timers := make([]*fakeTimer, 0, len(c.timers))
	for t := range c.timers {
		timers = append(timers, t)
	}
	c.mu.Unlock()
	for _, t := range timers {
		t.fireIfDue()
	}
}

type fakeTimer struct {
	clock    *fakeClock
	ch       chan time.Time
	deadline time.Time
	fired    bool
	stopped  bool
	mu       sync.Mutex
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return false
	}
	active := !t.fired
	t.stopped = true
	return active
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	active := !t.stopped && !t.fired
	select {
	case <-t.ch:
	default:
	}
	fc := t.clock
	fc.mu.Lock()
	defer fc.mu.Unlock()
	t.deadline = fc.now.Add(d)
	t.fired = false
	t.stopped = false
	return active
}

func (t *fakeTimer) fireIfDue() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped || t.fired {
		return
	}
	fc := t.clock
	fc.mu.Lock()
	now := fc.now
	fc.mu.Unlock()
	if t.deadline.After(now) {
		return
	}
	select {
	case t.ch <- now:
	default:
	}
	t.fired = true
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRecordingArchiverArchivesAndDeletes(t *testing.T) {
	now := stripMonotonic(time.Now())
	clk := newFakeClock(now)
	recordingDir := t.TempDir()
	archiveDir := t.TempDir()

	oldTime := now.Add(-35 * 24 * time.Hour)
	newTime := now.Add(-10 * 24 * time.Hour)

	oldPath := createCast(t, recordingDir, "host-a/2025-01-01/session-000.cast", 128, oldTime)
	oldPath2 := createCast(t, recordingDir, "host-b/2025-01-02/session-001.cast", 256, oldTime)
	_ = createCast(t, recordingDir, "host-a/2025-02-01/session-002.cast", 64, newTime)

	arch := NewArchiveRunner(ArchiveConfig{
		Enabled:     true,
		SourceDir:   recordingDir,
		ArchiveDir:  archiveDir,
		MinAge:      30 * 24 * time.Hour,
		MaxBundles:  12,
		MaxRunBytes: 0,
		Jitter:      0,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), clk)

	summary, err := arch.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error: %v", err)
	}

	if summary.FilesArchived != 2 {
		t.Fatalf("FilesArchived = %d, want 2", summary.FilesArchived)
	}

	month := monthKey(oldTime)
	bundle := filepath.Join(archiveDir, "recordings-"+month+".tar.gz")
	entries := readBundleEntries(t, bundle)
	if len(entries) != 2 {
		t.Fatalf("bundle entries = %d, want 2", len(entries))
	}
	if _, ok := entries[relPath(recordingDir, oldPath)]; !ok {
		t.Fatalf("missing entry for %s", oldPath)
	}
	if _, ok := entries[relPath(recordingDir, oldPath2)]; !ok {
		t.Fatalf("missing entry for %s", oldPath2)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("archived file still present: %v", err)
	}
	if _, err := os.Stat(oldPath2); !os.IsNotExist(err) {
		t.Fatalf("second archived file still present: %v", err)
	}

	youngPath := filepath.Join(recordingDir, "host-a", "2025-02-01", "session-002.cast")
	if _, err := os.Stat(youngPath); err != nil {
		t.Fatalf("young file missing: %v", err)
	}
}

func TestRecordingArchiverRetentionPrunesOldBundles(t *testing.T) {
	recordingDir := t.TempDir()
	archiveDir := t.TempDir()

	// Create four bundle files spanning months
	files := []string{
		"recordings-2024-01.tar.gz",
		"recordings-2024-02.tar.gz",
		"recordings-2024-03.tar.gz",
		"recordings-2024-04.tar.gz",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(archiveDir, name), []byte("dummy"), 0o600); err != nil {
			t.Fatalf("write bundle %s: %v", name, err)
		}
	}

	arch := NewArchiveRunner(ArchiveConfig{
		Enabled:     true,
		SourceDir:   recordingDir,
		ArchiveDir:  archiveDir,
		MinAge:      24 * time.Hour,
		MaxBundles:  2,
		MaxRunBytes: 0,
		Jitter:      0,
	}, testLogger(), realClock{})

	summary, err := arch.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error: %v", err)
	}
	if summary.BundlesPruned != 2 {
		t.Fatalf("BundlesPruned = %d, want 2", summary.BundlesPruned)
	}

	remaining := map[string]bool{}
	dirs, _ := os.ReadDir(archiveDir)
	for _, d := range dirs {
		remaining[d.Name()] = true
	}
	if len(remaining) != 2 || !remaining["recordings-2024-04.tar.gz"] || !remaining["recordings-2024-03.tar.gz"] {
		t.Fatalf("unexpected remaining bundles: %v", remaining)
	}
}

func TestRecordingArchiverFailureDoesNotDeleteSources(t *testing.T) {
	now := stripMonotonic(time.Now())
	recordingDir := t.TempDir()
	archiveDir := t.TempDir()
	old := now.Add(-40 * 24 * time.Hour)
	castPath := createCast(t, recordingDir, "host-a/2025-01-01/session-000.cast", 64, old)

	// Make archive dir read-only to force failure
	if err := os.Chmod(archiveDir, 0o500); err != nil {
		t.Fatalf("chmod archive dir: %v", err)
	}

	arch := NewArchiveRunner(ArchiveConfig{
		Enabled:     true,
		SourceDir:   recordingDir,
		ArchiveDir:  archiveDir,
		MinAge:      30 * 24 * time.Hour,
		MaxBundles:  12,
		MaxRunBytes: 0,
		Jitter:      0,
	}, testLogger(), realClock{})

	_, err := arch.RunOnce(context.Background())
	if err == nil {
		t.Fatal("expected error due to read-only archive dir")
	}

	if _, statErr := os.Stat(castPath); statErr != nil {
		t.Fatalf("source file removed on failure: %v", statErr)
	}

	// Restore permissions for cleanup
	_ = os.Chmod(archiveDir, 0o700)
}

func TestRecordingArchiverMaxRunBytes(t *testing.T) {
	now := stripMonotonic(time.Now())
	clk := newFakeClock(now)
	recordingDir := t.TempDir()
	archiveDir := t.TempDir()
	old := now.Add(-60 * 24 * time.Hour)

	first := createCast(t, recordingDir, "host-a/2024-01-01/session-000.cast", 100, old)
	second := createCast(t, recordingDir, "host-a/2024-01-01/session-001.cast", 200, old.Add(time.Minute))
	_ = createCast(t, recordingDir, "host-a/2024-01-01/session-002.cast", 300, old.Add(2*time.Minute))

	arch := NewArchiveRunner(ArchiveConfig{
		Enabled:     true,
		SourceDir:   recordingDir,
		ArchiveDir:  archiveDir,
		MinAge:      24 * time.Hour,
		MaxBundles:  12,
		MaxRunBytes: 250,
		Jitter:      0,
	}, testLogger(), clk)

	summary, err := arch.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error: %v", err)
	}
	if !summary.Capped {
		t.Fatalf("expected capped run")
	}
	if summary.FilesArchived != 1 { // only first file fits before cap trips
		t.Fatalf("FilesArchived = %d, want 1", summary.FilesArchived)
	}

	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("first file not removed: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("second file should remain for next run: %v", err)
	}
	third := filepath.Join(recordingDir, "host-a", "2024-01-01", "session-002.cast")
	if _, err := os.Stat(third); err != nil {
		t.Fatalf("third file should remain for next run: %v", err)
	}
}

func TestParseBundleMonth(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"recordings-2024-12.tar.gz", true},
		{"recordings-2024-00.tar.gz", false},
		{"recordings-2024-13.tar.gz", false},
		{"something-else.tar.gz", false},
	}

	for _, tt := range tests {
		_, ok := parseBundleMonth(tt.name)
		if ok != tt.valid {
			t.Fatalf("parseBundleMonth(%s) = %v, want %v", tt.name, ok, tt.valid)
		}
	}
}

func createCast(t *testing.T, baseDir, rel string, size int, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(baseDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	data := bytes.Repeat([]byte{'x'}, size)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

func readBundleEntries(t *testing.T, path string) map[string]int64 {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	defer func() { _ = f.Close() }()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	entries := make(map[string]int64)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		entries[hdr.Name] = hdr.Size
	}
}

func relPath(base, target string) string {
	rel, _ := filepath.Rel(base, target)
	return filepath.ToSlash(rel)
}
