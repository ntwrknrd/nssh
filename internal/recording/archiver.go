//go:build unix

package recording

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

// Clock provides wall-clock time and timers to the archive runner.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) ClockTimer
}

// ClockTimer is the timer interface used by Clock.
type ClockTimer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

type realClock struct{}

func (realClock) Now() time.Time {
	return stripMonotonic(time.Now())
}

func (realClock) NewTimer(d time.Duration) ClockTimer {
	return &realTimer{t: time.NewTimer(d)}
}

type realTimer struct {
	t *time.Timer
}

func (rt *realTimer) C() <-chan time.Time { return rt.t.C }

func (rt *realTimer) Stop() bool { return rt.t.Stop() }

func (rt *realTimer) Reset(d time.Duration) bool { return rt.t.Reset(d) }

func stripMonotonic(t time.Time) time.Time {
	return time.Unix(t.Unix(), int64(t.Nanosecond()))
}

// ArchiveConfig is the internal configuration used by the recording archiver.
type ArchiveConfig struct {
	SourceDir   string
	ArchiveDir  string
	MinAge      time.Duration
	MaxBundles  int
	MaxRunBytes int64
}

type archiveCandidate struct {
	absPath string
	relPath string
	size    int64
	modTime time.Time
}

// ArchiveSummary captures per-run results for logging and tests.
type ArchiveSummary struct {
	FilesArchived  int
	BytesArchived  int64
	FilesDeleted   int
	BytesDeleted   int64
	BundlesWritten int
	BundlesPruned  int
	Capped         bool
	SkippedReason  string
}

// ArchiveMaintenanceConfig is the public maintenance wrapper around archive policy.
type ArchiveMaintenanceConfig struct {
	Archive      config.SessionArchiveConfig
	RecordingDir string
	StateDir     string
}

// ArchiveRunner handles recording archival.
type ArchiveRunner struct {
	cfg    ArchiveConfig
	logger *slog.Logger
	clock  Clock
	mu     sync.Mutex
}

// NewArchiveRunner creates a recording archive runner.
func NewArchiveRunner(cfg ArchiveConfig, logger *slog.Logger, clk Clock) *ArchiveRunner {
	if logger == nil {
		logger = slog.Default()
	}
	if clk == nil {
		clk = realClock{}
	}
	return &ArchiveRunner{
		cfg:    cfg,
		logger: logger,
		clock:  clk,
	}
}

// RunArchive runs one explicit recording archive maintenance pass.
func RunArchive(ctx context.Context, maint ArchiveMaintenanceConfig, logger *slog.Logger) (ArchiveSummary, error) {
	var summary ArchiveSummary
	if logger == nil {
		logger = slog.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := maint.Archive.Validate(); err != nil {
		return summary, err
	}
	paths := config.DefaultPaths()
	recordingDir := maint.RecordingDir
	if recordingDir == "" {
		recordingDir = paths.RecordingsDir
	}
	stateDir := maint.StateDir
	if stateDir == "" {
		stateDir = paths.StateDir
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return summary, fmt.Errorf("create state dir: %w", err)
	}

	lockFile, locked, err := acquireArchiveLock(filepath.Join(stateDir, "recording-archive.lock"))
	if err != nil {
		return summary, err
	}
	if !locked {
		summary.SkippedReason = "archive already running"
		return summary, nil
	}
	defer func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
	}()

	timeout := maint.Archive.Timeout.Duration()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	return NewArchiveRunner(ArchiveConfig{
		SourceDir:   recordingDir,
		ArchiveDir:  maint.Archive.Dir,
		MinAge:      maint.Archive.MinAge.Duration(),
		MaxBundles:  maint.Archive.MaxBundles,
		MaxRunBytes: maint.Archive.MaxRunBytes,
	}, logger, nil).RunOnce(ctx)
}

func acquireArchiveLock(path string) (*os.File, bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open archive lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock archive maintenance: %w", err)
	}
	return file, true, nil
}

// RunOnce performs a single archival maintenance pass.
func (r *ArchiveRunner) RunOnce(ctx context.Context) (ArchiveSummary, error) {
	var summary ArchiveSummary

	if r == nil {
		return summary, nil
	}
	if r.cfg.SourceDir == "" {
		return summary, fmt.Errorf("recording source directory is empty")
	}
	if r.cfg.ArchiveDir == "" {
		return summary, fmt.Errorf("recording archive directory is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	// Ensure archive directory exists early.
	if err := os.MkdirAll(r.cfg.ArchiveDir, 0o700); err != nil {
		return summary, fmt.Errorf("create archive dir: %w", err)
	}

	now := r.clock.Now()
	candidates, capped, err := r.discoverCandidates(ctx, now)
	summary.Capped = capped
	if err != nil {
		return summary, err
	}

	groups := r.groupByMonth(candidates)
	var runErr error
	for month, files := range groups {
		select {
		case <-ctx.Done():
			return summary, ctx.Err()
		default:
		}

		monthSummary, err := r.processMonth(ctx, month, files)
		summary = summary.merge(monthSummary)
		if err != nil {
			runErr = err
			r.logger.Warn("recording archive month failed", "month", month, "err", err)
		}
	}

	pruned, err := r.pruneBundles()
	summary.BundlesPruned = pruned
	if err != nil && runErr == nil {
		runErr = err
	}

	return summary, runErr
}

func (r *ArchiveRunner) processMonth(ctx context.Context, month string, files []archiveCandidate) (ArchiveSummary, error) {
	var summary ArchiveSummary
	if len(files) == 0 {
		return summary, nil
	}

	bundleName := fmt.Sprintf("recordings-%s.tar.gz", month)
	bundlePath := filepath.Join(r.cfg.ArchiveDir, bundleName)

	existing, err := r.readArchiveIndex(bundlePath)
	if err != nil {
		return summary, fmt.Errorf("read archive index: %w", err)
	}

	var toAdd []archiveCandidate
	var already []archiveCandidate
	for _, c := range files {
		select {
		case <-ctx.Done():
			return summary, ctx.Err()
		default:
		}

		if size, ok := existing[c.relPath]; ok {
			if size == c.size {
				already = append(already, c)
				continue
			}
			r.logger.Warn("archive entry size mismatch; skipping re-add", "path", c.relPath, "existing", size, "current", c.size)
			continue
		}
		toAdd = append(toAdd, c)
	}

	if len(toAdd) == 0 {
		deleted := r.deleteSources(already)
		summary.FilesDeleted = deleted.Files
		summary.BytesDeleted = deleted.Bytes
		return summary, nil
	}

	bytesWritten, err := r.rewriteArchive(ctx, bundlePath, toAdd)
	if err != nil {
		return summary, err
	}

	summary.FilesArchived = len(toAdd)
	summary.BytesArchived = bytesWritten
	summary.BundlesWritten = 1

	toDelete := append([]archiveCandidate(nil), toAdd...)
	toDelete = append(toDelete, already...)
	deleted := r.deleteSources(toDelete)
	summary.FilesDeleted = deleted.Files
	summary.BytesDeleted = deleted.Bytes

	return summary, nil
}

func (r *ArchiveRunner) deleteSources(files []archiveCandidate) (result struct {
	Files int
	Bytes int64
}) {
	for _, c := range files {
		if err := os.Remove(c.absPath); err != nil {
			r.logger.Warn("failed to delete archived recording", "path", c.absPath, "err", err)
			continue
		}
		result.Files++
		result.Bytes += c.size
	}
	return result
}

func (r *ArchiveRunner) rewriteArchive(ctx context.Context, bundlePath string, toAdd []archiveCandidate) (int64, error) {
	temp, err := os.CreateTemp(r.cfg.ArchiveDir, "recordings-*.tar.gz")
	if err != nil {
		return 0, fmt.Errorf("create temp archive: %w", err)
	}
	defer func() { _ = os.Remove(temp.Name()) }()

	gw, err := gzip.NewWriterLevel(temp, gzip.BestSpeed)
	if err != nil {
		_ = temp.Close()
		return 0, fmt.Errorf("gzip writer: %w", err)
	}
	tw := tar.NewWriter(gw)

	if err := r.copyExistingArchive(ctx, bundlePath, tw); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		_ = temp.Close()
		return 0, err
	}

	var bytesWritten int64
	for _, c := range toAdd {
		if err := ctx.Err(); err != nil {
			_ = tw.Close()
			_ = gw.Close()
			_ = temp.Close()
			return bytesWritten, err
		}

		info, err := os.Stat(c.absPath)
		if err != nil {
			_ = tw.Close()
			_ = gw.Close()
			_ = temp.Close()
			return bytesWritten, fmt.Errorf("stat %s: %w", c.absPath, err)
		}
		if !info.Mode().IsRegular() {
			_ = tw.Close()
			_ = gw.Close()
			_ = temp.Close()
			return bytesWritten, fmt.Errorf("non-regular file %s", c.absPath)
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			_ = tw.Close()
			_ = gw.Close()
			_ = temp.Close()
			return bytesWritten, fmt.Errorf("header %s: %w", c.absPath, err)
		}
		hdr.Name = c.relPath
		hdr.ModTime = c.modTime
		hdr.Size = c.size

		if err := tw.WriteHeader(hdr); err != nil {
			_ = tw.Close()
			_ = gw.Close()
			_ = temp.Close()
			return bytesWritten, fmt.Errorf("write header %s: %w", c.relPath, err)
		}

		f, err := os.Open(c.absPath)
		if err != nil {
			_ = tw.Close()
			_ = gw.Close()
			_ = temp.Close()
			return bytesWritten, fmt.Errorf("open %s: %w", c.absPath, err)
		}
		n, err := io.Copy(tw, f)
		_ = f.Close()
		if err != nil {
			_ = tw.Close()
			_ = gw.Close()
			_ = temp.Close()
			return bytesWritten, fmt.Errorf("copy %s: %w", c.relPath, err)
		}
		if n != c.size {
			_ = tw.Close()
			_ = gw.Close()
			_ = temp.Close()
			return bytesWritten, fmt.Errorf("copy %s: wrote %d, expected %d", c.relPath, n, c.size)
		}
		bytesWritten += n
	}

	if err := tw.Close(); err != nil {
		_ = gw.Close()
		_ = temp.Close()
		return bytesWritten, fmt.Errorf("close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		_ = temp.Close()
		return bytesWritten, fmt.Errorf("close gzip: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return bytesWritten, fmt.Errorf("sync archive: %w", err)
	}
	if err := temp.Close(); err != nil {
		return bytesWritten, fmt.Errorf("close archive: %w", err)
	}

	if err := os.Rename(temp.Name(), bundlePath); err != nil {
		return bytesWritten, fmt.Errorf("replace archive: %w", err)
	}

	return bytesWritten, nil
}

func (r *ArchiveRunner) copyExistingArchive(ctx context.Context, bundlePath string, tw *tar.Writer) error {
	current, err := os.Open(bundlePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open existing archive: %w", err)
	}
	defer func() { _ = current.Close() }()

	gr, err := gzip.NewReader(current)
	if err != nil {
		return fmt.Errorf("read existing archive: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}
		clone := *hdr
		if err := tw.WriteHeader(&clone); err != nil {
			return fmt.Errorf("copy archive header %s: %w", hdr.Name, err)
		}
		n, err := io.Copy(tw, tr)
		if err != nil {
			return fmt.Errorf("copy archive body %s: %w", hdr.Name, err)
		}
		if n != hdr.Size {
			return fmt.Errorf("existing archive entry %s truncated: wrote %d want %d", hdr.Name, n, hdr.Size)
		}
	}
}

func (r *ArchiveRunner) readArchiveIndex(path string) (map[string]int64, error) {
	entries := make(map[string]int64)

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return entries, nil
		}
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, fmt.Errorf("scan archive: %w", err)
		}
		entries[hdr.Name] = hdr.Size
	}
}

func (r *ArchiveRunner) discoverCandidates(ctx context.Context, now time.Time) ([]archiveCandidate, bool, error) {
	if r.cfg.SourceDir == "" {
		return nil, false, fmt.Errorf("recording source directory is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := os.Stat(r.cfg.SourceDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("stat source dir: %w", err)
	}

	threshold := now.Add(-r.cfg.MinAge)
	var files []archiveCandidate

	walkErr := filepath.WalkDir(r.cfg.SourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".cast") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mod := stripMonotonic(info.ModTime())
		if !mod.Before(threshold) {
			return nil
		}

		rel, err := filepath.Rel(r.cfg.SourceDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "../") {
			return nil
		}

		files = append(files, archiveCandidate{
			absPath: path,
			relPath: rel,
			size:    info.Size(),
			modTime: mod,
		})
		return nil
	})
	if walkErr != nil {
		return nil, false, walkErr
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].relPath < files[j].relPath
		}
		return files[i].modTime.Before(files[j].modTime)
	})

	if r.cfg.MaxRunBytes <= 0 {
		return files, false, nil
	}

	var selected []archiveCandidate
	var total int64
	for _, f := range files {
		if total+f.size > r.cfg.MaxRunBytes {
			return selected, true, nil
		}
		selected = append(selected, f)
		total += f.size
	}

	return selected, false, nil
}

func (r *ArchiveRunner) groupByMonth(files []archiveCandidate) map[string][]archiveCandidate {
	groups := make(map[string][]archiveCandidate)
	for _, f := range files {
		key := monthKey(f.modTime)
		groups[key] = append(groups[key], f)
	}
	return groups
}

func monthKey(t time.Time) string {
	return stripMonotonic(t).UTC().Format("2006-01")
}

func parseBundleMonth(name string) (time.Time, bool) {
	const prefix = "recordings-"
	const suffix = ".tar.gz"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return time.Time{}, false
	}
	key := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if len(key) != 7 {
		return time.Time{}, false
	}
	parts := strings.Split(key, "-")
	if len(parts) != 2 {
		return time.Time{}, false
	}
	year, err1 := strconv.Atoi(parts[0])
	monthInt, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || monthInt < 1 || monthInt > 12 {
		return time.Time{}, false
	}
	return time.Date(year, time.Month(monthInt), 1, 0, 0, 0, 0, time.UTC), true
}

func (r *ArchiveRunner) pruneBundles() (int, error) {
	entries, err := os.ReadDir(r.cfg.ArchiveDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("list bundles: %w", err)
	}

	type bundleInfo struct {
		name  string
		month time.Time
	}
	var bundles []bundleInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		month, ok := parseBundleMonth(e.Name())
		if !ok {
			continue
		}
		bundles = append(bundles, bundleInfo{
			name:  filepath.Join(r.cfg.ArchiveDir, e.Name()),
			month: month,
		})
	}

	sort.Slice(bundles, func(i, j int) bool {
		return bundles[i].month.After(bundles[j].month)
	})

	if len(bundles) <= r.cfg.MaxBundles {
		return 0, nil
	}

	prune := bundles[r.cfg.MaxBundles:]
	deleted := 0
	for _, b := range prune {
		if err := os.Remove(b.name); err != nil {
			r.logger.Warn("failed to delete old recording bundle", "path", b.name, "err", err)
			continue
		}
		deleted++
	}
	return deleted, nil
}

func (s ArchiveSummary) merge(other ArchiveSummary) ArchiveSummary {
	s.FilesArchived += other.FilesArchived
	s.BytesArchived += other.BytesArchived
	s.FilesDeleted += other.FilesDeleted
	s.BytesDeleted += other.BytesDeleted
	s.BundlesWritten += other.BundlesWritten
	s.BundlesPruned += other.BundlesPruned
	s.Capped = s.Capped || other.Capped
	return s
}
