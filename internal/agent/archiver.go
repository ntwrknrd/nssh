//go:build linux || darwin

package agent

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// archiveConfig is the internal configuration used by the recording archiver.
type archiveConfig struct {
	enabled     bool
	sourceDir   string
	archiveDir  string
	minAge      time.Duration
	maxBundles  int
	maxRunBytes int64
	jitter      time.Duration
}

type archiveCandidate struct {
	absPath string
	relPath string
	size    int64
	modTime time.Time
}

// archiveSummary captures per-run results for logging and tests.
type archiveSummary struct {
	FilesArchived  int
	BytesArchived  int64
	FilesDeleted   int
	BytesDeleted   int64
	BundlesWritten int
	BundlesPruned  int
	Capped         bool
}

// recordingArchiver handles periodic recording archival.
type recordingArchiver struct {
	cfg    archiveConfig
	logger *slog.Logger
	clock  clock
	rand   *rand.Rand
	mu     sync.Mutex
}

func newRecordingArchiver(cfg archiveConfig, logger *slog.Logger, clk clock) *recordingArchiver {
	if logger == nil {
		logger = slog.Default()
	}
	if clk == nil {
		clk = realClock{}
	}
	seed := clk.Now().UnixNano()
	return &recordingArchiver{
		cfg:    cfg,
		logger: logger,
		clock:  clk,
		rand:   rand.New(rand.NewSource(seed)),
	}
}

func (r *recordingArchiver) enabled() bool {
	return r != nil && r.cfg.enabled && r.cfg.sourceDir != "" && r.cfg.archiveDir != ""
}

// runLoop executes RunOnce on a daily cadence with jitter until the context is canceled.
func (r *recordingArchiver) runLoop(ctx context.Context) {
	if !r.enabled() {
		return
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// First run occurs shortly after start to catch up if the agent was down.
	delay := r.initialDelay()

	for {
		timer := r.clock.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
		}

		if summary, err := r.RunOnce(ctx); err != nil {
			r.logger.Warn("recording archive run failed", "err", err)
		} else {
			r.logger.Debug("recording archive run complete",
				"files_archived", summary.FilesArchived,
				"bytes_archived", summary.BytesArchived,
				"files_deleted", summary.FilesDeleted,
				"bundles_written", summary.BundlesWritten,
				"bundles_pruned", summary.BundlesPruned,
				"capped", summary.Capped)
		}

		delay = r.nextInterval()
	}
}

// RunOnce performs a single archival maintenance pass.
func (r *recordingArchiver) RunOnce(ctx context.Context) (archiveSummary, error) {
	var summary archiveSummary

	if !r.enabled() {
		return summary, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	// Ensure archive directory exists early.
	if err := os.MkdirAll(r.cfg.archiveDir, 0o700); err != nil {
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

func (r *recordingArchiver) processMonth(ctx context.Context, month string, files []archiveCandidate) (archiveSummary, error) {
	var summary archiveSummary
	if len(files) == 0 {
		return summary, nil
	}

	bundleName := fmt.Sprintf("recordings-%s.tar.gz", month)
	bundlePath := filepath.Join(r.cfg.archiveDir, bundleName)

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

func (r *recordingArchiver) deleteSources(files []archiveCandidate) (result struct {
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

func (r *recordingArchiver) rewriteArchive(ctx context.Context, bundlePath string, toAdd []archiveCandidate) (int64, error) {
	temp, err := os.CreateTemp(r.cfg.archiveDir, "recordings-*.tar.gz")
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

func (r *recordingArchiver) copyExistingArchive(ctx context.Context, bundlePath string, tw *tar.Writer) error {
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

func (r *recordingArchiver) readArchiveIndex(path string) (map[string]int64, error) {
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

func (r *recordingArchiver) discoverCandidates(ctx context.Context, now time.Time) ([]archiveCandidate, bool, error) {
	if r.cfg.sourceDir == "" {
		return nil, false, fmt.Errorf("recording source directory is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := os.Stat(r.cfg.sourceDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("stat source dir: %w", err)
	}

	threshold := now.Add(-r.cfg.minAge)
	var files []archiveCandidate

	walkErr := filepath.WalkDir(r.cfg.sourceDir, func(path string, d fs.DirEntry, err error) error {
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

		rel, err := filepath.Rel(r.cfg.sourceDir, path)
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

	if r.cfg.maxRunBytes <= 0 {
		return files, false, nil
	}

	var selected []archiveCandidate
	var total int64
	for _, f := range files {
		if total+f.size > r.cfg.maxRunBytes {
			return selected, true, nil
		}
		selected = append(selected, f)
		total += f.size
	}

	return selected, false, nil
}

func (r *recordingArchiver) groupByMonth(files []archiveCandidate) map[string][]archiveCandidate {
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

func (r *recordingArchiver) pruneBundles() (int, error) {
	entries, err := os.ReadDir(r.cfg.archiveDir)
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
			name:  filepath.Join(r.cfg.archiveDir, e.Name()),
			month: month,
		})
	}

	sort.Slice(bundles, func(i, j int) bool {
		return bundles[i].month.After(bundles[j].month)
	})

	if len(bundles) <= r.cfg.maxBundles {
		return 0, nil
	}

	prune := bundles[r.cfg.maxBundles:]
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

// initialDelay returns a random delay between 0 and jitter before the first run.
func (r *recordingArchiver) initialDelay() time.Duration {
	if r.cfg.jitter <= 0 {
		return 0
	}
	return time.Duration(r.rand.Int63n(int64(r.cfg.jitter) + 1))
}

// nextInterval returns 24h +/- jitter for the next run.
func (r *recordingArchiver) nextInterval() time.Duration {
	base := 24 * time.Hour
	j := r.cfg.jitter
	if j <= 0 {
		return base
	}
	span := int64(j) * 2
	shift := r.rand.Int63n(span+1) - int64(j)
	return base + time.Duration(shift)
}

func (s archiveSummary) merge(other archiveSummary) archiveSummary {
	s.FilesArchived += other.FilesArchived
	s.BytesArchived += other.BytesArchived
	s.FilesDeleted += other.FilesDeleted
	s.BytesDeleted += other.BytesDeleted
	s.BundlesWritten += other.BundlesWritten
	s.BundlesPruned += other.BundlesPruned
	s.Capped = s.Capped || other.Capped
	return s
}
